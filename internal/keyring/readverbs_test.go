package keyring

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/vault"
)

// The read verbs on an encrypted archive (design-encrypted-archives-phase3 §7.4,
// §7.5, E7; P3-2a-3 and P3-2a-4, MA-797).

// covers: MA-797, R37
// extract copies an encrypted archive's preserved originals out, byte for byte,
// writing nothing into the archive; a -dest inside an encrypted archive is refused
// writing nothing there; a record with no original is counted and skipped (exit 3),
// as a plaintext archive's is.
func TestExtractCopiesAnEncryptedArchivesOriginalsOut(t *testing.T) {
	freshRecord(t)
	out, _, w := made(t, nil)
	readBackup(t, w)
	before := treeOf(t, out)
	dest := filepath.Join(t.TempDir(), "extracted")
	rep, err := Extract(w, app.FormatEML, dest, false, quiet, nil)
	if err != nil || rep.Emitted != 2 || rep.ExitCode() != 0 {
		t.Fatalf("extract: %+v, %v", rep, err)
	}
	var emls []string
	filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, ".eml") {
			b, _ := os.ReadFile(p)
			emls = append(emls, string(b))
		}
		return nil
	})
	joined := strings.Join(emls, "\n")
	if len(emls) != 2 || !strings.Contains(joined, "Message-ID: <abc@example.com>") || !strings.Contains(joined, "just plain text here") {
		t.Fatalf("the originals extracted: %d files\n%s", len(emls), joined)
	}
	after := treeOf(t, out)
	delete(after, lockfile.Name) // the archive lock extract holds, as on a plaintext archive
	delete(before, lockfile.Name)
	if !reflect.DeepEqual(after, before) {
		t.Fatal("extract wrote into the encrypted archive")
	}

	inside := filepath.Join(out, "extracted")
	_, err = Extract(w, app.FormatEML, inside, false, quiet, nil)
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("encrypted archive"),
		assure.NoSideEffect(func() bool { return !exists(inside) }))

	// A PST's records keep no original: counted and skipped.
	_, _, w2 := made(t, nil)
	backedUp(t, w2)
	rep, err = Extract(w2, app.FormatMbox, filepath.Join(t.TempDir(), "x"), false, quiet, nil)
	assure.Reached(t, err == nil && rep.Emitted == 0 && rep.SkippedNoEML > 0 && rep.ExitCode() == app.ExtractPartial, "no originals: partial")
}

// covers: MA-797, R37
// Keyed reindex rebuilds an encrypted archive's search index from its sealed units
// and never removes a message — a unit gone is not a deletion; a paused archive is
// refused; stopped before its save, nothing is saved (E7, the owner's ruling).
func TestKeyedReindexRebuildsTheIndexAndRemovesNothing(t *testing.T) {
	freshRecord(t)
	out, _, w := made(t, nil)
	readBackup(t, w)
	r, err := ReaderSource(w, ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rec := aMessage(t, r)
	r.Lock()
	// A message's page unit gone: reindex keeps its record.
	k, _, _ := w.acquire()
	unitRel, err := (&export.Units{Root: out, Key: k}).Locate(rec.Unit, vault.UnitMessage, *rec.Sealed.HTML)
	w.release()
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(out, unitRel))

	rep, err := Reindex(context.Background(), w, quiet)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := ReaderSource(w, ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Lock()
	v, _ := r2.ensure()
	if _, kept := v.m.KeyForPath(rec.Path); !kept || v.m.Len() != 2 || rep.Added+rep.Unreadable == 0 {
		t.Fatalf("reindex removed a message or rebuilt nothing: %d records, %+v", v.m.Len(), rep)
	}
	var total int
	r2.UseIndex(func(ix *index.Index) error { _, total, err = ix.Search(index.Query{Limit: 10}); return err })
	assure.Reached(t, total > 0, "the rebuilt index searches")

	// Stopped before it starts: nothing is done, the archive's lock not even taken;
	// stopped mid-run: nothing is saved — the record's pointer is as it was.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	read := func(name string) string { b, _ := os.ReadFile(filepath.Join(out, name)); return string(b) }
	beforeStop, lockBefore := read(container.PointerName), read(lockfile.Name)
	if _, err := Reindex(ctx, w, quiet); !errors.Is(err, context.Canceled) || read(container.PointerName) != beforeStop || read(lockfile.Name) != lockBefore {
		t.Fatalf("a reindex stopped before it starts: %v", err)
	}
	if _, err := Reindex(&stopAfter{Context: context.Background(), n: 1}, w, quiet); !errors.Is(err, context.Canceled) || read(container.PointerName) != beforeStop {
		t.Fatalf("a reindex stopped mid-run: %v", err)
	}

	// A paused archive is refused.
	if err := seal.Seal(out, "paused for the test"); err != nil {
		t.Fatal(err)
	}
	snap := treeOf(t, out)
	_, err = Reindex(context.Background(), w, quiet)
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.NoSideEffect(func() bool { return reflect.DeepEqual(treeOf(t, out), snap) }))
}

// covers: MA-797, MA-759, R37
// An index above the reader's memory budget is worked on in the reader's own
// scratch file, outside the archive, named by its process and load; the file goes
// when the reader locks; a file a reader that is gone left is removed by the next
// keyed open, a live one's is kept, and uninstall owns the shape (§7.4).
func TestALargeIndexIsReadFromTheReadersOwnScratchFile(t *testing.T) {
	orig := readerIndexBudget
	readerIndexBudget = 1
	defer func() { readerIndexBudget = orig }()
	_, u, r, ts := readerFor(t, ReaderOptions{})
	v, err := r.ensure()
	if err != nil || v.ix == nil {
		t.Fatalf("the scratch index: %v %v", err, v.ixErr)
	}
	scratch := scratchFiles(t)
	if len(scratch) != 1 || !app.ReaderScratchName.MatchString(filepath.Base(scratch[0])) {
		t.Fatalf("scratch files: %v", scratch)
	}
	if code, _, body := get(t, ts, "/api/search?q=hello"); code != 200 || !strings.Contains(body, "Hello HTML") {
		t.Fatalf("search from the scratch index: %d %s", code, body)
	}

	// A dead reader's leftover is swept when a reader opens; the live one is kept.
	dead := filepath.Join(filepath.Dir(scratch[0]), "reader-999999999-1.db")
	os.WriteFile(dead, []byte("left"), 0o600)
	other, err := ReaderSource(u, ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	other.Lock()
	if exists(dead) || !exists(scratch[0]) {
		t.Fatal("the sweep removed a live reader's file or kept a dead one's")
	}
	for wait := time.Now().Add(10 * time.Second); len(scratchFiles(t)) > 1 && time.Now().Before(wait); {
		time.Sleep(20 * time.Millisecond) // the other reader's own file goes at its lock
	}

	// A load again gets a file of its own; the old view's goes, the new one's stays.
	r.reload(v)
	v2, err := r.ensure()
	if err != nil || v2 == v || v2.ix == nil {
		t.Fatalf("the reload: %v", err)
	}
	gone := func(p string) bool {
		deadline := time.Now().Add(10 * time.Second)
		for exists(p) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		return !exists(p)
	}
	if !gone(scratch[0]) {
		t.Fatal("the old view's scratch file stayed")
	}
	now := scratchFiles(t)
	if len(now) != 1 || now[0] == scratch[0] {
		t.Fatalf("after the reload: %v", now)
	}
	if code, _, body := get(t, ts, "/api/search?q=hello"); code != 200 || !strings.Contains(body, "Hello HTML") {
		t.Fatalf("search after the reload: %d %s", code, body)
	}

	r.Lock()
	assure.Reached(t, gone(now[0]), "the scratch file removed at the lock")
}

// stopAfter is a context stopped after its first n looks at Err.
type stopAfter struct {
	context.Context
	n int
}

func (s *stopAfter) Err() error {
	if s.n > 0 {
		s.n--
		return nil
	}
	return context.Canceled
}

// scratchFiles lists the reader scratch indexes under the sandboxed cache.
func scratchFiles(t *testing.T) []string {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	filepath.WalkDir(cache, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), "reader-") {
			files = append(files, p)
		}
		return nil
	})
	return files
}
