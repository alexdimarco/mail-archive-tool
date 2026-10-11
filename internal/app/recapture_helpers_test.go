package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// The helpers the re-capture rows share. A row builds an archive with the real
// pipeline (a maildir read by Run, or messages handed to an Exporter), degrades its
// source or interrupts its run, and asks what the archive then holds against what
// its manifest recorded.

const rcStore = "Store"

func rcLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// rcMail is one RFC 822 message as a maildir file holds it: an html body and, when
// attName is set, one attachment of attData (empty data is an attachment the
// source declares but does not deliver). id is the Message-ID without brackets.
func rcMail(id, subject, body, attName string, attData []byte) []byte {
	hdr := "From: Alice <alice@example.com>\r\nTo: bob@example.com\r\nSubject: " + subject +
		"\r\nMessage-ID: <" + id + ">\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\nMIME-Version: 1.0\r\n"
	if attName == "" {
		return []byte(hdr + "Content-Type: text/html; charset=utf-8\r\n\r\n" + body + "\r\n")
	}
	return []byte(hdr + "Content-Type: multipart/mixed; boundary=\"B\"\r\n\r\n" +
		"--B\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" + body + "\r\n" +
		"--B\r\nContent-Type: application/pdf; name=\"" + attName + "\"\r\n" +
		"Content-Disposition: attachment; filename=\"" + attName + "\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		base64.StdEncoding.EncodeToString(attData) + "\r\n--B--\r\n")
}

// rcSource is an empty maildir folder; rcPut writes (or rewrites) its one message.
func rcSource(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(tmpDir(t), "Inbox")
	if err := os.MkdirAll(filepath.Join(dir, "cur"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func rcPut(t *testing.T, src string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(src, "cur", "m1"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// rcRun is a CLI-shaped run over the maildir. A partial outcome (a gap observed) is
// an outcome, not a failure.
func rcRun(t *testing.T, src, out string, mode export.Mode, raw bool, progress ProgressFunc) Result {
	t.Helper()
	res, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: mode, KeepRaw: raw}, rcLogger(), progress)
	var partial *PartialError
	if err != nil && !errors.As(err, &partial) {
		t.Fatalf("run: %v", err)
	}
	return res
}

// rcRunIndexed is rcRun with the search index kept, so a run's uncommitted index
// batch is part of what an interruption leaves.
func rcRunIndexed(t *testing.T, src, out string, mode export.Mode, raw bool, progress ProgressFunc) Result {
	t.Helper()
	res, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: mode, KeepRaw: raw, Index: true}, rcLogger(), progress)
	var partial *PartialError
	if err != nil && !errors.As(err, &partial) {
		t.Fatalf("run: %v", err)
	}
	return res
}

// rcVerify is `verify` over an archive.
func rcVerify(t *testing.T, out string) Report {
	t.Helper()
	rep, err := Verify(out, VerifyOptions{}, rcLogger(), nil)
	if err != nil {
		t.Fatalf("verify %s: %v", out, err)
	}
	return rep
}

// rcDigest is the fixity of the file at path, and whether it exists.
func rcDigest(t *testing.T, path string) (state.FileDigest, bool) {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return state.FileDigest{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return state.FileDigest{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(b))}, true
}

// rcAsRecorded lists what is wrong with the files rec records, read from disk
// under out: an expected file that is absent, or whose bytes differ from the
// recorded digest. The page is always expected; the zip and the original when a
// digest of them is recorded. An empty list means the archive holds exactly what
// rec says it holds.
func rcAsRecorded(t *testing.T, out string, rec state.Record) []string {
	t.Helper()
	fx := assure.Reached(t, rec.Fixity, "the record's fixity")
	stem := strings.TrimSuffix(rec.Path, ".html")
	var bad []string
	for _, f := range []struct {
		rel  string
		want *state.FileDigest
	}{
		{stem + ".html", fx.HTML},
		{stem + "-attachments.zip", fx.Zip},
		{stem + ".eml", fx.EML},
	} {
		if f.want == nil {
			continue
		}
		got, ok := rcDigest(t, filepath.Join(out, filepath.FromSlash(f.rel)))
		switch {
		case !ok:
			bad = append(bad, f.rel+" is gone")
		case got != *f.want:
			bad = append(bad, f.rel+" no longer has its recorded bytes")
		}
	}
	return bad
}

// rcSnapshot copies the archive as it stands, from inside a callback: the state a
// hard kill at that instant leaves (RC31). Everything on disk is copied: the
// manifest, the index with its -wal and -shm, the history, every message file. The
// lock file is copied by its holder line, not its bytes (a held lock is a mandatory
// byte-range lock on Windows): the copy is a new file nobody holds, which is the
// stale lock a killed run leaves behind (the lock is an OS lock, released when its
// holder dies).
func rcSnapshot(t *testing.T, out string) string {
	t.Helper()
	snap := tmpDir(t)
	err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(out, p)
		if rerr != nil {
			return rerr
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(snap, rel), 0o755)
		}
		if d.Name() == lockfile.Name {
			return os.WriteFile(filepath.Join(snap, rel), []byte(lockfile.Holder(p)+"\n"), 0o600)
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(filepath.Join(snap, rel), b, 0o644)
	})
	if err != nil {
		t.Fatalf("snapshot of %s: %v", out, err)
	}
	return snap
}

// rcAssertKilledState is the harness's own check (RC31, MA-358): the snapshot is the
// residue of a run killed inside it, not the archive of a run that finished. The
// persisted manifest is byte for byte the one the run started from, and the lock
// file is present and held by no one (a stale lock). A harness that observed
// post-commit state fails here, instead of passing the encoder vacuously.
func rcAssertKilledState(t *testing.T, snap string, manifestBefore []byte) {
	t.Helper()
	if got := rcRead(t, snap, ".mailarchive-manifest.json"); !bytes.Equal(got, manifestBefore) {
		t.Fatalf("the snapshot's manifest is not the one the run started from: the snapshot observed saved state, not the state a kill leaves")
	}
	lock := filepath.Join(snap, lockfile.Name)
	if _, err := os.Stat(lock); err != nil || lockfile.Held(lock) || lockfile.Holder(lock) == "" {
		t.Fatalf("the snapshot should hold the stale lock file of the killed run (stat %v, held %v, holder %q)", err, lockfile.Held(lock), lockfile.Holder(lock))
	}
}

// rcIndexNames is the set of paths the index at out holds rows for.
func rcIndexNames(t *testing.T, out string) map[string]bool {
	t.Helper()
	ix, err := index.OpenReadonly(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatalf("open the index of %s: %v", out, err)
	}
	defer ix.Close()
	rows := map[string]bool{}
	if err := ix.EachRow(func(_ int64, _, path string) error { rows[path] = true; return nil }); err != nil {
		t.Fatal(err)
	}
	return rows
}

// rcRenameStem moves a record's files to another stem and persists the manifest
// naming them: the shape of an archive written under an earlier naming rule, which
// a full re-export renames (the README's recommended re-export of a v0.1 or v0.2
// archive). Its fixity is unchanged, since the bytes are.
func rcRenameStem(t *testing.T, out, key, base string) state.Record {
	t.Helper()
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	man, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := man.Get(key)
	if !ok {
		t.Fatalf("no record %q to rename", key)
	}
	oldStem := filepath.Join(out, filepath.FromSlash(strings.TrimSuffix(rec.Path, ".html")))
	newStem := filepath.Join(filepath.Dir(oldStem), base)
	for _, suffix := range []string{".html", "-attachments.zip", ".eml"} {
		if err := os.Rename(oldStem+suffix, newStem+suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
	}
	rec.Path = filepath.ToSlash(filepath.Join(filepath.Dir(rec.Path), base+".html"))
	man.Add(key, rec)
	if err := man.Save(); err != nil {
		t.Fatal(err)
	}
	return rec
}

// rcMsg is a message handed straight to an Exporter, for the cases whose fault (a
// stream that errors, a write that is refused) a maildir cannot carry.
func rcMsg(id, subject, body string, raw []byte, atts ...model.Attachment) *model.Message {
	d := time.Date(2025, 3, 3, 9, 0, 0, 0, time.UTC)
	return &model.Message{Subject: subject, SenderEmail: "alice@example.com", To: "bob@example.com",
		Received: d, IdentityDate: d, InternetMessageID: "<" + id + ">", HTMLBody: body, Raw: raw, Attachments: atts}
}

// rcBlob is an attachment whose bytes, failure and read-time side effect the test
// controls between runs.
type rcBlob struct {
	data  []byte
	err   error  // the stream delivers half of data, then fails: a torn read
	hook  func() // runs when the stream is read, the instant a fault can be injected
	cid   string
	ctype string
}

func (b *rcBlob) att(name string) model.Attachment {
	ct := b.ctype
	if ct == "" {
		ct = "application/pdf"
	}
	return model.Attachment{Filename: name, MimeType: ct, ContentID: b.cid, WriteTo: func(w io.Writer) (int64, error) {
		if b.hook != nil {
			b.hook()
		}
		if b.err != nil {
			n, _ := w.Write(b.data[:len(b.data)/2])
			return int64(n), b.err
		}
		n, err := w.Write(b.data)
		return int64(n), err
	}}
}

// rcExport runs one message through a fresh Exporter over the archive's manifest
// and, when the export succeeds, saves the manifest as a finished run would.
func rcExport(t *testing.T, out string, mode export.Mode, raw bool, m *model.Message) (export.Stats, error) {
	t.Helper()
	man, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	exp := &export.Exporter{OutDir: out, Manifest: man, Mode: mode, KeepRaw: raw, Log: rcLogger()}
	if _, err := exp.Export(rcStore, []string{"Inbox"}, m); err != nil {
		return exp.Stats, err
	}
	if err := man.Save(); err != nil {
		t.Fatal(err)
	}
	return exp.Stats, nil
}

// rcPNG is a valid 1x1 PNG, the bytes of an inline image the page embeds.
var rcPNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")

// rcWritten is a record's page, zip and original as written: each file's bytes and
// modification time, so a run that rewrites a file with identical bytes still shows
// (a re-capture that writes nothing leaves the inode's mtime alone).
func rcWritten(t *testing.T, out string, rec state.Record) map[string]string {
	t.Helper()
	stem := strings.TrimSuffix(rec.Path, ".html")
	got := map[string]string{}
	for _, suffix := range []string{".html", "-attachments.zip", ".eml"} {
		p := filepath.Join(out, filepath.FromSlash(stem+suffix))
		fi, err := os.Stat(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		sum, _ := rcDigest(t, p)
		got[stem+suffix] = sum.SHA256 + "@" + fi.ModTime().UTC().Format(time.RFC3339Nano)
	}
	return got
}

// rcSum is the fixity of b.
func rcSum(b []byte) state.FileDigest {
	sum := sha256.Sum256(b)
	return state.FileDigest{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(b))}
}

// rcLegacy rewrites the record of the message with id into what a version-1
// manifest holds: its path and the `unknown` sentinel, with no fingerprint and no
// fixity, so the next incremental run re-examines it once.
func rcLegacy(t *testing.T, out, id string) {
	t.Helper()
	key, rec := recordForID(t, out, id)
	man, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	man.Add(key, state.Record{Path: rec.Path, Folder: rec.Folder, Missing: []string{state.UnknownSentinel}})
	if err := man.Save(); err != nil {
		t.Fatal(err)
	}
}

func rcRead(t *testing.T, out, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rcHas(list []string, s string) bool { return slices.Contains(list, s) }
