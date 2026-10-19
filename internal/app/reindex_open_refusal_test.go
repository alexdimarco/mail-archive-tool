package app

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-386, R12, S33
// Plain `reindex` gives the same open refusals as a run (F5): it named the
// rebuild for every cause, "rebuild it" for a busy index (held 5 s, then the
// wrong advice) and for a newer format (the inner text said upgrade, the outer
// rebuild), with -out unquoted. Now a busy index names the retry, a newer one
// the upgrade, a damaged one the rebuild with the folder quoted; none of them
// writes. Positive twin: plain reindex over the healthy index succeeds.
func TestReindexOpenRefusalNamesTheCause(t *testing.T) {
	opts, _, unchanged := unwritableFixture(t, 2)
	out, idxPath := opts.Out, filepath.Join(opts.Out, "search.db")
	if _, _, err := Reindex(out, discard()); err != nil {
		t.Fatalf("positive twin: reindex over the healthy index failed: %v", err)
	}
	unchanged = snapshotOf(t, out)

	// Busy: a foreign connection holds search.db exclusively past the wait.
	holder, err := sql.Open("sqlite", idxPath)
	if err != nil {
		t.Fatal(err)
	}
	holder.SetMaxOpenConns(1)
	if _, err := holder.Exec(`PRAGMA locking_mode=EXCLUSIVE; BEGIN; UPDATE meta SET version = version; COMMIT;`); err != nil {
		t.Fatal(err)
	}
	_, _, rerr := Reindex(out, discard())
	holder.Close()
	code, msg := refusalOf(rerr)
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(idxPath, "busy", "close any other program", "run"),
		assure.Forbid("reindex -rebuild", "rebuild it"),
		assure.NoSideEffect(unchanged))

	// Newer format.
	db, err := sql.Open("sqlite", idxPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM meta; INSERT INTO meta(version) VALUES(99);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	unchanged = snapshotOf(t, out)
	_, _, rerr = Reindex(out, discard())
	code, msg = refusalOf(rerr)
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(idxPath, "newer mailarchive", "upgrade"),
		assure.Forbid("reindex -rebuild", "rebuild it"),
		assure.NoSideEffect(unchanged))

	// Damaged, manifest present: the rebuild, the folder quoted.
	if err := os.WriteFile(idxPath, []byte(strings.Repeat("not a sqlite database ", 200)), 0o644); err != nil {
		t.Fatal(err)
	}
	unchanged = snapshotOf(t, out)
	_, _, rerr = Reindex(out, discard())
	code, msg = refusalOf(rerr)
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(idxPath, fmt.Sprintf("reindex -rebuild -out %q", out)),
		assure.NoSideEffect(unchanged))
}

// snapshotOf returns a check that search.db and the manifest in out hold the
// bytes they hold now.
func snapshotOf(t *testing.T, out string) func() bool {
	t.Helper()
	read := func() (string, string) {
		db, _ := os.ReadFile(filepath.Join(out, "search.db"))
		m, _ := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
		return string(db), string(m)
	}
	d0, m0 := read()
	return func() bool { d, m := read(); return d == d0 && m == m0 }
}
