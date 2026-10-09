package index

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-387, R8, S2
// A failed Add is isolated to its own row (F49): the other rows of the same
// uncommitted batch — before and after it — are kept and searchable once the
// batch commits, and the failing row leaves no half-written trace. Two fault
// shapes are injected (fault injection only; the index is the real one): a
// docs insert refused by a trigger, and a docs_fts insert refused AFTER its docs
// row was written (a pre-planted docs_fts row occupies the rowid), which must
// not leave an orphan docs row without its FTS twin (INT-2).
func TestAddFailureIsIsolatedToItsRow(t *testing.T) {
	ix, err := Open(filepath.Join(tmpDir(t), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if _, err := ix.db.Exec(`CREATE TRIGGER inject_fault BEFORE INSERT ON docs WHEN NEW.subject = 'poison'
		BEGIN SELECT RAISE(ABORT, 'injected index fault'); END`); err != nil {
		t.Fatal(err)
	}

	when := time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC)
	const n, poisonAt, ftsClashAt = 50, 45, 20
	var failed []int
	for i := 0; i < n; i++ {
		subject := fmt.Sprintf("message%02d", i)
		if i == poisonAt {
			subject = "poison"
		}
		if i == ftsClashAt {
			// The next docs rowid is i+1 (rows 1..i are in; none failed yet), so
			// planting a docs_fts row there makes this row's FTS insert fail
			// after its docs insert succeeded.
			inBatch(t, ix, `INSERT INTO docs_fts(rowid, subject, body) VALUES(?, 'planted', 'planted')`, i+1)
		}
		if err := ix.Add("s", []string{"Inbox"}, mkMsg(subject, "bob", "me", "body of "+subject, when), fmt.Sprintf("s/Inbox/%d.html", i), fmt.Sprintf("k%d", i)); err != nil {
			failed = append(failed, i)
		}
		if i == ftsClashAt {
			// Unplant it so the next row (which reuses the freed rowid) is clean.
			inBatch(t, ix, `DELETE FROM docs_fts WHERE rowid = ?`, i+1)
		}
	}
	if fmt.Sprint(failed) != fmt.Sprint([]int{ftsClashAt, poisonAt}) {
		t.Fatalf("failed rows = %v, want exactly [%d %d]", failed, ftsClashAt, poisonAt)
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}

	got, err := ix.Count()
	if err != nil {
		t.Fatal(err)
	}
	if got != n-2 {
		t.Fatalf("index holds %d rows after one batch with 2 failed Adds, want %d — the failures took other rows with them", got, n-2)
	}
	// The rows on both sides of each failure are searchable.
	for _, i := range []int{0, ftsClashAt - 1, ftsClashAt + 1, poisonAt - 1, poisonAt + 1, n - 1} {
		_, total, err := ix.Search(Query{Text: fmt.Sprintf("message%02d", i)})
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 {
			t.Errorf("message%02d: %d hit(s), want 1", i, total)
		}
	}
	// No orphan: every docs row has its docs_fts twin, and the failed rows'
	// keys are absent from docs.
	var orphans int
	if err := ix.db.QueryRow(`SELECT count(*) FROM docs WHERE id NOT IN (SELECT rowid FROM docs_fts)`).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Errorf("%d docs row(s) without a docs_fts twin", orphans)
	}
	var failedKeys int
	if err := ix.db.QueryRow(`SELECT count(*) FROM docs WHERE key IN (?, ?)`, fmt.Sprintf("k%d", ftsClashAt), fmt.Sprintf("k%d", poisonAt)).Scan(&failedKeys); err != nil {
		t.Fatal(err)
	}
	if failedKeys != 0 {
		t.Errorf("%d failed row(s) were committed anyway", failedKeys)
	}
	assure.Reached(t, got, "rows kept around the failed Adds")
}

// inBatch runs a fault-injection statement on the index's single connection:
// inside the open batch transaction when there is one (the connection is held
// by it), else directly.
func inBatch(t *testing.T, ix *Index, q string, args ...any) {
	t.Helper()
	var err error
	if ix.tx != nil {
		_, err = ix.tx.Exec(q, args...)
	} else {
		_, err = ix.db.Exec(q, args...)
	}
	if err != nil {
		t.Fatal(err)
	}
}
