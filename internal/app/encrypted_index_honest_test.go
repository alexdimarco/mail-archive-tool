package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// failIndexAdds makes indexAdd refuse the next n messages, for the test's length.
func failIndexAdds(t *testing.T, n int) *int {
	t.Helper()
	add := indexAdd
	failed := new(int)
	indexAdd = func(ix *index.Index, s string, f []string, m *model.Message, rel, k string) error {
		if *failed < n {
			*failed++
			return errors.New("the index could not take this message")
		}
		return add(ix, s, f, m, rel, k)
	}
	t.Cleanup(func() { indexAdd = add })
	return failed
}

// covers: MA-761, R37
// A catch-up that cannot add a message it read keeps the index marked behind, so a
// later run adds it — never "every message again" with one left out for good; a
// keyed rebuild that cannot add one fails with the index kind. A record whose sealed
// units cannot be read is still left out without holding the mark (design §6).
func TestACatchUpThatCannotAddAMessageKeepsTheIndexBehind(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	failed := failIndexAdds(t, 1)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatalf("a catch-up that could not add a message stopped the backup: %v", err)
	}
	if *failed != 1 {
		t.Fatalf("fixture: %d add(s) refused, want the catch-up's first", *failed)
	}
	m, s, idx := sealedIndexOf(t, out, key)
	if n, _ := idx.Count(); !s.IndexBehind() || n != m.Len()-1 {
		t.Fatalf("the catch-up left out one message (%d of %d indexed) and must keep the index behind: %v", n, m.Len(), s.IndexBehind())
	}
	*failed = 0
	if _, err := RebuildEncrypted(context.Background(), Options{Out: out, Key: key}, discard()); FailureKind(err) != state.FailIndex {
		t.Fatalf("a rebuild that could not add a message: want the index kind, got %v (%s)", err, FailureKind(err))
	}
	*failed = 1 // the one refusal is spent
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	requireWholeIndex(t, out, key)
}

// covers: MA-761, R37
// A first indexed run of an encrypted archive whose new index cannot be opened — its
// scratch folder cannot be written — still backs up: the run goes on without its
// index, says why, marks it behind, and the next run that can open it catches up.
func TestAFreshIndexThatCannotBeOpenedStillBacksUp(t *testing.T) {
	forceScratchIndex(t)
	store := filepath.Join(tmpDir(t), "store")
	mbox := filepath.Join(store, "Inbox")
	p14Write(t, mbox, indexMbox("first", "one"))
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	path, _ := indexScratchPath(out, key.ArchiveID())
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Dir(path), 0o700) })
	p14Write(t, mbox, indexMbox("first", "one")+indexMbox("second", "two"))
	var said bytes.Buffer
	if _, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Key: key, Index: true}, log.New(&said, "", 0), nil); err != nil {
		t.Fatalf("an index that could not be opened stopped the backup: %v", err)
	}
	if m := loadSealed(t, out, key); m.Len() != 2 {
		t.Fatalf("the record holds %d messages, want both backed up", m.Len())
	}
	if !strings.Contains(said.String(), "the search index is not updated this run") {
		t.Fatalf("the run must say why its index was not updated:\n%s", said.String())
	}
	if _, s, _ := sealedIndexOf(t, out, key); !s.IndexBehind() {
		t.Fatal("an index the run could not open must be marked behind")
	}
	os.Chmod(filepath.Dir(path), 0o700)
	if _, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	requireWholeIndex(t, out, key)
}

// manyMessages is an mbox of n messages with bodies long enough to grow an index.
func manyMessages(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(indexMbox(fmt.Sprintf("message %d", i), fmt.Sprintf("m%d", i)))
		for j := 0; j < 40; j++ {
			fmt.Fprintf(&b, "word%d-%d alpha%d beta%d gamma%d\n", i, j, i*j, i+j, j)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// emptyIndexSize is how many bytes a new in-memory index takes.
func emptyIndexSize(t *testing.T) int64 {
	t.Helper()
	ix, err := index.OpenMemory(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	n, err := ix.MemorySize()
	if err != nil || n <= 0 {
		t.Fatalf("an empty index's size %d (%v)", n, err)
	}
	return n
}

// covers: MA-761, R37
// A first backup starts with nothing, so its index starts in memory — and moves to
// its scratch file once it outgrows the memory budget during the run, instead of
// growing in memory without bound; the run's commits seal it whole, the archive holds
// no search.db, and the file is gone when the run ends. With no room for the file the
// index takes no more messages that run and is marked behind; the backup goes on, and
// the next run with room catches it up.
func TestAFirstBackupThatOutgrowsMemoryMovesItsIndexToTheScratchFile(t *testing.T) {
	budget, every := indexMemoryBudget, budgetCheckEvery
	indexMemoryBudget, budgetCheckEvery = emptyIndexSize(t), 1 // a new index fits; a grown one does not
	t.Cleanup(func() { indexMemoryBudget, budgetCheckEvery = budget, every })
	for _, room := range []bool{true, false} {
		store := filepath.Join(tmpDir(t), "store")
		p14Write(t, filepath.Join(store, "Inbox"), manyMessages(30))
		out, key := encryptedOut(t)
		path, _ := indexScratchPath(out, key.ArchiveID())
		inFile := 0
		add := indexAdd
		indexAdd = func(ix *index.Index, s string, f []string, m *model.Message, rel, k string) error {
			err := add(ix, s, f, m, rel, k)
			if _, serr := os.Stat(path); serr == nil {
				inFile++
			}
			return err
		}
		free := scratchFree
		if !room {
			scratchFree = func(string) (uint64, error) { return 1 << 20, nil }
		}
		var said bytes.Buffer
		_, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Key: key, Index: true}, log.New(&said, "", 0), nil)
		indexAdd, scratchFree = add, free
		if err != nil {
			t.Fatalf("room %v: %v", room, err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("room %v: the scratch index outlived the run (%v)", room, err)
		}
		for _, p := range treeOf(t, out) {
			if strings.Contains(p, "search.db") {
				t.Fatalf("room %v: the archive holds %s", room, p)
			}
		}
		if room {
			if inFile == 0 {
				t.Fatal("the index outgrew the memory budget and was never moved to its scratch file")
			}
			requireWholeIndex(t, out, key)
			continue
		}
		if inFile != 0 || !strings.Contains(said.String(), "the search index is not updated for the rest of this run") {
			t.Fatalf("with no room the index must stay out of the scratch folder (%d) and the run say so:\n%s", inFile, said.String())
		}
		if _, s, _ := sealedIndexOf(t, out, key); !s.IndexBehind() {
			t.Fatal("an index that stopped taking messages must be marked behind")
		}
		if _, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
			t.Fatal(err)
		}
		requireWholeIndex(t, out, key)
	}
}

// covers: MA-763, R37
// An encrypted archive's backup that finishes with its search index still missing
// messages is recorded as a success with the index_behind kind — the one thing about
// the index a keyless status can read — and a backup that leaves it whole records no
// kind.
func TestABackupThatEndsWithItsIndexBehindIsRecordedSo(t *testing.T) {
	forceScratchIndex(t)
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	free := scratchFree
	scratchFree = func(string) (uint64, error) { return 1 << 20, nil }
	_, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil)
	scratchFree = free
	if err != nil {
		t.Fatal(err)
	}
	lr, st, err := state.ReadLastRun(out)
	if err != nil || st != state.LastRunPresent || lr.Status != state.RunOK || lr.ErrorKind != state.NoteIndexBehind || !lr.Encrypted {
		t.Fatalf("a backup that left its index behind recorded %+v (%v)", lr, err)
	}
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	if lr, _, _ = state.ReadLastRun(out); lr.Status != state.RunOK || lr.ErrorKind != "" {
		t.Fatalf("a backup that caught its index up recorded %+v", lr)
	}
	h, err := state.ReadRunHistory(out)
	if err != nil || len(h) != 2 || h[0].ErrorKind != state.NoteIndexBehind || h[1].ErrorKind != "" {
		t.Fatalf("the backup history must carry the kind on the first run only: %+v (%v)", h, err)
	}
}

// covers: MA-759, R37
// The estimate for the archive's records alone moves its index to a scratch file:
// indexing turned on for an archive with many records and no index yet — an image of
// nothing — is caught up in a scratch file, not in memory.
func TestTheRecordEstimateAloneMovesTheIndexToScratch(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	per := indexBytesPerRecord
	indexBytesPerRecord = indexMemoryBudget // two records are over the budget; no image is
	t.Cleanup(func() { indexBytesPerRecord = per })
	path, _ := indexScratchPath(out, key.ArchiveID())
	inFile, adds := 0, 0
	add := indexAdd
	indexAdd = func(ix *index.Index, s string, f []string, m *model.Message, rel, k string) error {
		err := add(ix, s, f, m, rel, k)
		adds++
		if _, serr := os.Stat(path); serr == nil {
			inFile++
		}
		return err
	}
	t.Cleanup(func() { indexAdd = add })
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	if adds < 2 || inFile != adds {
		t.Fatalf("%d of %d messages were added in the scratch file, want all", inFile, adds)
	}
	requireWholeIndex(t, out, key)
}

// covers: MA-758, MA-759, R37
// A Microsoft 365 capture keeps the sealed index whole as a local run does: a capture
// with indexing off marks it behind and the next indexed one catches it up; a message
// a capture could not add marks it behind and the next capture adds it; an index first
// loaded mid-capture and found damaged is healed in the same capture; and above the
// memory budget the capture works in a scratch file that is gone when it ends.
func TestAGraphCaptureKeepsTheSealedIndexWhole(t *testing.T) {
	s := sweepServer(false, []string{"A1", "A2"}, []string{"B1"}, nil)
	defer s.Close()
	out, key := encryptedOut(t)
	capture := func(index bool) {
		t.Helper()
		if _, err := RunGraph(context.Background(), isoOptions(s), Options{Out: out, Key: key, Index: index}, discard()); err != nil {
			t.Fatal(err)
		}
	}
	behind := func() bool {
		t.Helper()
		_, st, _ := sealedIndexOf(t, out, key)
		return st.IndexBehind()
	}
	capture(true)
	requireWholeIndex(t, out, key)
	s.AddMessage("u1", "F_IN", "A3")
	capture(false)
	if !behind() {
		t.Fatal("a capture with indexing off must mark the sealed index behind")
	}
	capture(true)
	requireWholeIndex(t, out, key)

	s.AddMessage("u1", "F_IN", "A4")
	failIndexAdds(t, 1)
	capture(true)
	if !behind() {
		t.Fatal("a message a capture could not add must mark the sealed index behind")
	}
	if lr, _, _ := state.ReadLastRun(out); lr.Status != state.RunOK || lr.ErrorKind != state.NoteIndexBehind {
		t.Fatalf("a capture that left its index behind must record so: %+v", lr)
	}
	capture(true)
	requireWholeIndex(t, out, key)

	damageIndexChunk(t, out, key)
	s.AddMessage("u1", "F_SE", "B2")
	capture(true) // the new message loads the index mid-capture, and finds it damaged
	requireWholeIndex(t, out, key)

	forceScratchIndex(t)
	path, _ := indexScratchPath(out, key.ArchiveID())
	s.AddMessage("u1", "F_SE", "B3")
	capture(true)
	requireWholeIndex(t, out, key)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the capture's scratch index outlived it (%v)", err)
	}

	// A new index that cannot be opened (its scratch folder unwritable) never stops a
	// capture: records captured with indexing off, then an indexed capture.
	fresh, fkey := encryptedOut(t)
	if _, err := RunGraph(context.Background(), isoOptions(s), Options{Out: fresh, Key: fkey}, discard()); err != nil {
		t.Fatal(err)
	}
	fpath, _ := indexScratchPath(fresh, fkey.ArchiveID())
	if err := os.MkdirAll(filepath.Dir(fpath), 0o700); err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.Dir(fpath), 0o500)
	t.Cleanup(func() { os.Chmod(filepath.Dir(fpath), 0o700) })
	s.AddMessage("u1", "F_SE", "B4")
	if _, err := RunGraph(context.Background(), isoOptions(s), Options{Out: fresh, Key: fkey, Index: true}, discard()); err != nil {
		t.Fatalf("a new index that could not be opened stopped the capture: %v", err)
	}
	if _, st, _ := sealedIndexOf(t, fresh, fkey); !st.IndexBehind() {
		t.Fatal("an index the capture could not open must be marked behind")
	}
}

// covers: MA-762, R37
// A scratch index a crashed run left is removed by the archive's next keyed run even
// when that run has indexing off.
func TestACrashLeftScratchIndexIsRemovedByARunWithIndexingOff(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	path, _ := indexScratchPath(out, key.ArchiveID())
	p14Write(t, path, "the plaintext index a crashed run left")
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a run with indexing off must still remove a crashed run's scratch index (%v)", err)
	}
}

// covers: MA-762, R37
// Two archives never share a scratch folder — not even two whose folders differ only
// in case, which the canonical source id folds together — while one archive at one
// location always has the same one.
func TestTwoArchivesNeverShareAScratchFolder(t *testing.T) {
	_, a := encryptedOut(t)
	_, b := encryptedOut(t)
	base := tmpDir(t)
	upper, lower := filepath.Join(base, "Mail"), filepath.Join(base, "mail")
	da, _ := archiveScratch(upper, a.ArchiveID())
	db, _ := archiveScratch(lower, b.ArchiveID())
	if da == db {
		t.Fatal("two archives whose folders differ only in case share a scratch folder")
	}
	if again, _ := archiveScratch(upper, a.ArchiveID()); again != da {
		t.Fatal("one archive at one location must keep its scratch folder")
	}
	if other, _ := archiveScratch(filepath.Join(base, "elsewhere"), a.ArchiveID()); other == da {
		t.Fatal("two copies of one archive must not share a scratch folder")
	}
}

// covers: MA-762, R37
// A sealed index that does not close cleanly at the end of a run — its scratch file
// cannot be removed — is said in the run's log, never dropped in silence; a plaintext
// archive's index closes as before.
func TestAScratchIndexThatDoesNotCloseCleanlyIsSaid(t *testing.T) {
	img, err := (func() ([]byte, error) {
		ix, err := index.OpenMemory(nil)
		if err != nil {
			return nil, err
		}
		defer ix.Close()
		return ix.Image()
	})()
	if err != nil {
		t.Fatal(err)
	}
	for _, sealed := range []bool{true, false} {
		dir := filepath.Join(tmpDir(t), "index")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "search.db")
		ix, err := index.OpenScratch(path, func(w io.Writer) error { _, err := w.Write(img); return err }, 0, 3, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ix.Folders(); err != nil { // loads it into its scratch file
			t.Fatal(err)
		}
		os.Chmod(dir, 0o500)
		var said bytes.Buffer
		closeRunIndex(ix, sealed, log.New(&said, "", 0))
		os.Chmod(dir, 0o700)
		if got := strings.Contains(said.String(), "did not close cleanly"); got != sealed {
			t.Fatalf("sealed %v: the log said %q", sealed, said.String())
		}
	}
}
