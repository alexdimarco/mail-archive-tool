package index

import (
	"database/sql"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

func rawExec(t *testing.T, path, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("raw exec %q: %v", query, err)
	}
}

func rawMetaVersion(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v int
	if err := db.QueryRow(`SELECT version FROM meta LIMIT 1`).Scan(&v); err != nil {
		t.Fatalf("read meta version: %v", err)
	}
	return v
}

// keysOf returns every indexed row's (key -> path), used to assert the exact
// set of rows after a repair.
func keysOf(t *testing.T, ix *Index) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := ix.EachRow(func(_ int64, key, path string) error {
		out[key] = path
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// covers: MA-132, R5, R12
// A search index written by a newer mailarchive (a stored meta version above
// what this build understands) is refused by Open, naming the versions and the
// upgrade remedy, and the stored version is left unchanged (fail-closed).
// Positive twin first: an index this build wrote re-opens clean.
func TestIndexRefusesNewerVersion(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok.db")
	ix, err := Open(ok)
	if err != nil {
		t.Fatalf("a fresh index must open: %v", err)
	}
	ix.Close()
	ix2, err := Open(ok)
	if err != nil {
		t.Fatalf("re-open of a current index must succeed: %v", err)
	}
	ix2.Close()

	future := filepath.Join(dir, "future.db")
	seed, err := Open(future)
	if err != nil {
		t.Fatal(err)
	}
	seed.Close()
	rawExec(t, future, `DELETE FROM meta`)
	rawExec(t, future, `INSERT INTO meta(version) VALUES(3)`)

	_, err = Open(future)
	rc, msg := 0, ""
	if err != nil {
		rc, msg = 2, err.Error()
	}
	assure.Refused(t, rc, msg,
		assure.Names(future, "newer mailarchive", "upgrade"),
		assure.NoSideEffect(func() bool { return rawMetaVersion(t, future) == 3 }))
}

// covers: MA-131, R8, S30
// The index repair is the twin of the manifest re-scope: RepairKeys re-scopes
// every one-NUL (legacy) row from its own path, leaves two-NUL (current) rows
// untouched (never double-prefixed), and — when an old-binary excursion left a
// current row already holding a re-scoped row's target key — drops the stale
// row so exactly one row per key survives (R8 parity: one search hit per
// message). It is driven by force (the manifest re-key signal), the only
// trigger that survives a downgrade that never regresses the index's own
// version.
func TestRepairKeysRepairsMixedRows(t *testing.T) {
	ix, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()

	date := time.Unix(1_700_000_000, 0).UTC()
	rows := []struct{ folder, path, key string }{
		{"Inbox", "s/Inbox/a.html", "s\x00Inbox\x00mid:<a@x>"},     // v3, untouched
		{"Sent", "s/Sent/b.html", "Sent\x00mid:<b@x>"},             // v2, re-scoped
		{"Draft", "s/Draft/c-new.html", "s\x00Draft\x00mid:<c@x>"}, // v3, collision survivor-to-be
		{"Draft", "s/Draft/c-old.html", "Draft\x00mid:<c@x>"},      // v2, collides after re-scope
	}
	for i, r := range rows {
		m := mkMsg("subject", "s", "r", "body", date)
		if err := ix.Add("s", []string{r.folder}, m, r.path, r.key); err != nil {
			t.Fatalf("add row %d: %v", i, err)
		}
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}

	n, err := ix.RepairKeys(true, state.MigrateKey, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("RepairKeys re-keyed %d rows, want 2", n)
	}

	got := keysOf(t, ix)
	want := map[string]string{
		"s\x00Inbox\x00mid:<a@x>": "s/Inbox/a.html",
		"s\x00Sent\x00mid:<b@x>":  "s/Sent/b.html",
		"s\x00Draft\x00mid:<c@x>": "s/Draft/c-old.html", // the re-scoped (later) row won
	}
	if len(got) != len(want) {
		t.Fatalf("after repair: %d rows, want %d (%v)", len(got), len(want), got)
	}
	for k, p := range want {
		if got[k] != p {
			t.Errorf("row %q path = %q, want %q", k, got[k], p)
		}
	}
	assure.Reached(t, got, "rows after repair")
}
