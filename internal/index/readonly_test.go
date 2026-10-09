package index

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// buildReadonlyFixture writes a closed, at-rest search.db holding two messages
// into dir and returns its path.
func buildReadonlyFixture(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "search.db")
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC)
	if err := ix.Add("s", []string{"Inbox"}, mkMsg("Acme invoice", "bob", "me", "the invoice is attached", when), "s/Inbox/a.html", "k1"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Add("s", []string{"Inbox"}, mkMsg("Lunch", "carol", "me", "lunch tomorrow?", when), "s/Inbox/b.html", "k2"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// covers: MA-385, R8, S33
// OpenReadonly is truly read-only (F85/N14). On a writable archive the handle
// it returns cannot write the index and leaves search.db's bytes unchanged; on a
// read-only copy (write-protected media: neither the directory nor the file is
// writable, and no -wal/-shm sidecar exists) it still opens and searches — the
// old read-write open failed there with SQLITE_READONLY_DIRECTORY wrapped as
// "run an export first" — and it creates no sidecar file on that media.
func TestOpenReadonlyIsReadOnly(t *testing.T) {
	// Positive twin: a writable archive opens, searches, and refuses a write.
	dir := tmpDir(t)
	path := buildReadonlyFixture(t, dir)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadonly(path)
	if err != nil {
		t.Fatalf("OpenReadonly on a healthy writable archive: %v", err)
	}
	_, total, err := ro.Search(Query{Text: "invoice"})
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, total, "hits from the read-only handle on a writable archive")
	when := time.Date(2025, 3, 2, 9, 0, 0, 0, time.UTC)
	addErr := ro.Add("s", []string{"Inbox"}, mkMsg("forged", "x", "y", "forged row", when), "s/Inbox/f.html", "forged")
	if addErr == nil {
		addErr = ro.Flush()
	}
	if addErr == nil {
		t.Fatal("a write through the OpenReadonly handle succeeded — the index was opened read-write")
	}
	if err := ro.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("search.db changed on disk after a read-only open")
	}

	// Read-only media: the directory and the file are write-protected and no
	// sidecar exists (a WAL reader cannot create its -shm there).
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions model read-only media; Windows ignores a directory's read-only bit")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits, so read-only media cannot be modelled")
	}
	media := tmpDir(t)
	mpath := buildReadonlyFixture(t, media)
	for _, s := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(mpath + s); !os.IsNotExist(err) {
			t.Fatalf("fixture left a %s sidecar; the read-only-media case needs none", s)
		}
	}
	if err := os.Chmod(mpath, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(media, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(media, 0o755) })
	rom, err := OpenReadonly(mpath)
	if err != nil {
		t.Fatalf("OpenReadonly on a read-only archive copy: %v", err)
	}
	_, mtotal, err := rom.Search(Query{Text: "invoice"})
	if err != nil {
		t.Fatalf("search on a read-only archive copy: %v", err)
	}
	assure.Reached(t, mtotal, "hits from a read-only archive copy")
	if err := rom.Close(); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(mpath + s); !os.IsNotExist(err) {
			t.Errorf("a read-only open created %s on read-only media", filepath.Base(mpath+s))
		}
	}
}

// covers: MA-386, R12, S33
// A search.db that is present but cannot be read (corrupt) is refused with an
// accurate message: it names the file and the `reindex -rebuild` recovery, and
// never the misleading "run an export first" (F85). The refusal writes nothing:
// the damaged file's bytes are left exactly as they were.
func TestOpenReadonlyUnreadableIndexMessage(t *testing.T) {
	// Positive twin: a healthy index opens.
	okPath := buildReadonlyFixture(t, tmpDir(t))
	ok, err := OpenReadonly(okPath)
	if err != nil {
		t.Fatalf("healthy index refused: %v", err)
	}
	n, _ := ok.Count()
	assure.Reached(t, n, "rows in the healthy index")
	ok.Close()

	path := filepath.Join(tmpDir(t), "search.db")
	junk := bytes.Repeat([]byte("not a sqlite database, just junk bytes. "), 128)
	if err := os.WriteFile(path, junk, 0o644); err != nil {
		t.Fatal(err)
	}
	ix, oerr := OpenReadonly(path)
	code, msg := 0, ""
	if oerr != nil {
		code, msg = 1, oerr.Error()
	} else {
		ix.Close()
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(path, "reindex -rebuild"),
		assure.Forbid("run an export first"),
		assure.NoSideEffect(func() bool {
			got, err := os.ReadFile(path)
			return err == nil && bytes.Equal(got, junk)
		}))
}
