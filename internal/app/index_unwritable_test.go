package app

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
)

// refusalOf maps a run's error to the (exit, message) pair assure.Refused
// takes: the CLI exits 1 with the error's text on any refusal.
func refusalOf(err error) (int, string) {
	if err != nil {
		return 1, err.Error()
	}
	return 0, ""
}

// unwritableFixture is a healthy indexed archive of n maildir messages whose
// search.db was closed cleanly (no -wal/-shm beside it, the shape a scheduled
// job meets the next night), with a check that search.db and the manifest
// still hold the bytes they hold now.
func unwritableFixture(t *testing.T, n int) (opts Options, healthy Result, unchanged func() bool) {
	t.Helper()
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, n)
	out := tmpDir(t)
	opts = Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true}
	healthy, err := Run(context.Background(), opts, discard(), nil)
	if err != nil {
		t.Fatalf("positive twin: the run over a writable archive failed: %v", err)
	}
	assure.Reached(t, healthy.Indexed, "messages the healthy run indexed")
	idxPath := filepath.Join(out, "search.db")
	for _, s := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(idxPath + s); err == nil {
			t.Fatalf("fixture: a clean close left %s beside search.db", s)
		}
	}
	db0, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	m0, err := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	unchanged = func() bool {
		db, derr := os.ReadFile(idxPath)
		m, merr := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
		return derr == nil && merr == nil && bytes.Equal(db, db0) && bytes.Equal(m, m0)
	}
	return opts, healthy, unchanged
}

// noRebuildTemp reports whether no search.db.rebuild (or its -wal/-shm) is
// left in out.
func noRebuildTemp(out string) bool {
	for _, s := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(filepath.Join(out, "search.db.rebuild"+s)); err == nil {
			return false
		}
	}
	return true
}

// covers: MA-386, R12, S33
// The rebuild is never the remedy for a cause it cannot fix (F1). A run whose
// writable open of search.db meets an archive folder it cannot write — a
// read-only remount, changed permissions: SQLite answers
// SQLITE_READONLY_DIRECTORY (1544), it cannot create the -wal/-shm — is
// refused naming the file, SQLite's cause, the read-only folder and the remedy
// that cause has (make it writable), never `reindex -rebuild`, which fails the
// same way ("unable to open database file"). The refusal writes nothing:
// search.db and the manifest keep their bytes. The rebuild, run there anyway,
// names the same cause. The remedy, followed, heals: the next run indexes
// every record. Positive twin first: the run over the writable archive.
func TestIndexOpenRefusalNamesAReadOnlyArchiveFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits model a read-only folder; Windows ignores a directory's read-only bit")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits, so a read-only folder cannot be modelled")
	}
	opts, healthy, unchanged := unwritableFixture(t, 3)
	out, idxPath := opts.Out, filepath.Join(opts.Out, "search.db")

	if err := os.Chmod(out, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(out, 0o755) })
	_, err := Run(context.Background(), opts, discard(), nil)
	code, msg := refusalOf(err)
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(idxPath, "attempt to write a readonly database", "the archive folder "+out+" is read-only", "make it writable", "run again"),
		assure.Forbid("reindex -rebuild", "rebuild it"),
		assure.NoSideEffect(unchanged))

	// The rebuild meets the same folder: it names the same cause, never the
	// "out of memory (14)" SQLite's CANTOPEN reads as, and leaves no temp.
	_, rerr := Rebuild(out, discard())
	rcode, rmsg := refusalOf(rerr)
	assure.Refused(t, rcode, rmsg, assure.Code(1),
		assure.Names("search.db.rebuild", "the archive folder "+out+" is read-only", "make it writable"),
		assure.NoSideEffect(func() bool { return unchanged() && noRebuildTemp(out) }))

	// Followed: the folder writable again, the next run indexes every record.
	if err := os.Chmod(out, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), opts, discard(), nil)
	if err != nil || res.Indexed != res.ManifestSize || res.Indexed != healthy.Indexed {
		t.Fatalf("after making the folder writable: indexed=%d of %d, err=%v; want all %d", res.Indexed, res.ManifestSize, err, healthy.Indexed)
	}
}

// covers: MA-386, R12, S33
// A search.db the writer can open but not write is refused at the open (g7
// friction F7). With the file made read-only (0444) in a writable folder SQLite
// opens it read-only without a word, and the run used to go on: exit 0, one
// identical "attempt to write a readonly database (8)" warning per message,
// INDEX-ERRORS=N, the cause never named. Now the writable open probes for a write
// and the run is refused naming the file, SQLite's cause, that search.db is
// read-only and "make it writable" — never the rebuild — leaving search.db and
// the manifest unchanged and exporting nothing. Positive twin: the healthy run;
// then, followed (the file writable again), the next run indexes every record.
func TestIndexOpenRefusalNamesAReadOnlySearchDB(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits model a read-only file")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	opts, healthy, unchanged := unwritableFixture(t, 3)
	idxPath := filepath.Join(opts.Out, "search.db")
	if err := os.Chmod(idxPath, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(idxPath, 0o644) })

	// New mail to export, so a run that went on would produce index errors.
	maildirMessages(t, opts.Inputs[0], 6)
	var logs bytes.Buffer
	_, err := Run(context.Background(), opts, log.New(&logs, "", 0), nil)
	code, msg := refusalOf(err)
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(idxPath, "attempt to write a readonly database", "search.db is read-only", "make it writable", "run again"),
		assure.Forbid("reindex -rebuild", "rebuild it"),
		assure.NoSideEffect(unchanged))
	if strings.Contains(logs.String(), "warning: index:") {
		t.Fatalf("the run went on to index and warn per message:\n%s", logs.String())
	}
	// The refused open leaves no read-only -wal/-shm pair behind, which would
	// keep refusing after search.db itself is made writable.
	for _, s := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(idxPath + s); err == nil {
			t.Fatalf("the refused open left %s beside search.db", s)
		}
	}

	if err := os.Chmod(idxPath, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), opts, discard(), nil)
	if err != nil || res.IndexErrors != 0 || res.Indexed != res.ManifestSize || res.Indexed <= healthy.Indexed {
		t.Fatalf("after making search.db writable: indexed=%d of %d errors=%d err=%v", res.Indexed, res.ManifestSize, res.IndexErrors, err)
	}
}

// covers: MA-386, R12, S33
// A full disk is not a damaged index either (F1). SQLite's own answer to a
// write the disk refuses — SQLITE_FULL, here produced for real by a page cap
// (max_page_count), the code a really full disk can give — passed through the
// run's refusal names the file, SQLite's cause, the full disk and the remedy
// (free space), never the rebuild, which writes a whole new index to that same
// disk. Positive twin: the same refusal for a damaged file (NOTADB) still
// names the rebuild.
func TestIndexOpenRefusalNamesAFullDisk(t *testing.T) {
	out := tmpDir(t)
	idxPath := filepath.Join(out, "search.db")

	// Positive twin: a damaged file keeps the rebuild remedy (and the
	// manifest the rebuild needs is there).
	if err := os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), []byte(`{"version":5,"entries":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	junk := filepath.Join(tmpDir(t), "junk.db")
	if err := os.WriteFile(junk, bytes.Repeat([]byte("not a sqlite database "), 200), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Open(junk); err == nil {
		t.Fatal("fixture: a junk file opened as an index")
	} else if msg := indexOpenError(idxPath, out, err).Error(); !strings.Contains(msg, "mailarchive reindex -rebuild -out") {
		t.Fatalf("positive twin: a damaged file's refusal no longer names the rebuild: %q", msg)
	}

	capped, err := sql.Open("sqlite", filepath.Join(tmpDir(t), "capped.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer capped.Close()
	_, full := capped.Exec(`CREATE TABLE t(x); PRAGMA max_page_count = 2; INSERT INTO t VALUES (randomblob(65536));`)
	if full == nil || !strings.Contains(full.Error(), "full") {
		t.Fatalf("fixture: a page-capped insert did not answer SQLITE_FULL: %v", full)
	}
	code, msg := refusalOf(indexOpenError(idxPath, out, fmt.Errorf("index pragmas: %w", full)))
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(idxPath, "database or disk is full", "the disk holding "+out+" is full or failing", "free space", "run again"),
		assure.Forbid("reindex -rebuild", "rebuild it"))
}
