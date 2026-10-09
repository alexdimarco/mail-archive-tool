package app

import (
	"context"
	"database/sql"
	"io"
	"log"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
)

// injectIndexFault pre-creates out/search.db and plants a trigger that makes
// the docs insert of the message with the given subject fail with the given
// RAISE action (ABORT: only that statement fails; ROLLBACK: SQLite abandons the
// whole transaction, as it may on SQLITE_FULL/IOERR). Fault injection only:
// the index, exporter and run are the real ones.
func injectIndexFault(t *testing.T, out, subject, action string) {
	t.Helper()
	path := filepath.Join(out, "search.db")
	ix, err := index.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER inject_fault BEFORE INSERT ON docs WHEN NEW.subject = '` + subject + `'
		BEGIN SELECT RAISE(` + action + `, 'injected index fault'); END`); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-388, R8, R18, S29
// Every message a run exports is either in the search index or counted as an
// index error (F49), and the count lands in the last-run record. One message
// whose index insert fails costs exactly that one row — the other 49 of its
// batch stay indexed — and when SQLite abandons the whole transaction
// (RAISE(ROLLBACK), the shape of a disk-full/I-O failure) the earlier rows lost
// with it are counted too, never silently dropped. Result.Indexed is the
// index's total, so on a fresh archive Indexed + IndexErrors == exported; on
// an incremental run into an existing archive the run's own rows balance:
// (Indexed after − Indexed before) + IndexErrors == exported.
func TestRunCountsEveryIndexLoss(t *testing.T) {
	const n = 50
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, n)
	logger := log.New(io.Discard, "", 0)

	// Positive twin: a healthy run indexes every message, no index errors.
	okOut := tmpDir(t)
	okRes, err := Run(context.Background(), Options{Inputs: []string{src}, Out: okOut, Mode: export.Incremental, Index: true}, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	if okRes.Indexed != n || okRes.IndexErrors != 0 {
		t.Fatalf("healthy run: indexed=%d indexErrors=%d, want %d/0", okRes.Indexed, okRes.IndexErrors, n)
	}

	for _, tc := range []struct {
		action      string
		wantIndexed int // -1: any, as long as the books balance
		wantErrors  int // -1: any >1
	}{
		{"ABORT", n - 1, 1},
		{"ROLLBACK", -1, -1},
	} {
		out := tmpDir(t)
		injectIndexFault(t, out, "msg 7", tc.action)
		res, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true}, logger, nil)
		if err != nil {
			t.Fatalf("%s: run failed: %v", tc.action, err)
		}
		exported := assure.Reached(t, res.Stats.Exported, "messages exported")
		if res.Indexed+res.IndexErrors != exported {
			t.Errorf("%s: indexed=%d + indexErrors=%d != exported=%d — index rows were lost without being counted", tc.action, res.Indexed, res.IndexErrors, exported)
		}
		if tc.wantIndexed >= 0 && res.Indexed != tc.wantIndexed {
			t.Errorf("%s: indexed=%d, want %d (one failed insert must cost only its own row)", tc.action, res.Indexed, tc.wantIndexed)
		}
		if tc.wantErrors >= 0 && res.IndexErrors != tc.wantErrors {
			t.Errorf("%s: indexErrors=%d, want %d", tc.action, res.IndexErrors, tc.wantErrors)
		}
		if tc.wantErrors < 0 && res.IndexErrors <= 1 {
			t.Errorf("%s: indexErrors=%d, want the failing row plus the batch lost with it", tc.action, res.IndexErrors)
		}
		lr, st, err := state.ReadLastRun(out)
		if err != nil || st != state.LastRunPresent {
			t.Fatalf("%s: no last-run record: %v %v", tc.action, st, err)
		}
		if lr.IndexErrors != res.IndexErrors {
			t.Errorf("%s: last-run index_errors=%d, want %d", tc.action, lr.IndexErrors, res.IndexErrors)
		}
	}

	// Incremental: a second run into an archive that already holds 10 indexed
	// messages exports only the 10 new ones, and SQLite abandons its batch.
	// Result.Indexed is the index's total (what status shows), so the run's
	// own books are the growth of the index plus its IndexErrors.
	incSrc := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, incSrc, 10)
	out := tmpDir(t)
	opts := Options{Inputs: []string{incSrc}, Out: out, Mode: export.Incremental, Index: true}
	first, err := Run(context.Background(), opts, logger, nil)
	if err != nil || first.Indexed != 10 || first.IndexErrors != 0 {
		t.Fatalf("incremental: first run indexed=%d errors=%d err=%v, want 10/0", first.Indexed, first.IndexErrors, err)
	}
	maildirMessages(t, incSrc, 20) // the same 10 again, plus msg 10..19
	injectIndexFault(t, out, "msg 14", "ROLLBACK")
	second, err := Run(context.Background(), opts, logger, nil)
	if err != nil {
		t.Fatalf("incremental: second run failed: %v", err)
	}
	if second.Stats.Exported != 10 {
		t.Fatalf("incremental: second run exported %d, want the 10 new messages", second.Stats.Exported)
	}
	if second.IndexErrors <= 1 {
		t.Fatalf("incremental: indexErrors=%d, want the failing row plus the batch lost with it", second.IndexErrors)
	}
	if grew := second.Indexed - first.Indexed; grew+second.IndexErrors != second.Stats.Exported {
		t.Errorf("incremental: the index grew by %d and %d were counted as errors, but %d were exported — rows lost without being counted", grew, second.IndexErrors, second.Stats.Exported)
	}
	if shown := health.Gather(out, "").Indexed; second.Indexed != shown {
		t.Errorf("incremental: Result.Indexed=%d, but the index holds %d rows — Indexed must be the index's total", second.Indexed, shown)
	}
	if lr, _, _ := state.ReadLastRun(out); lr.IndexErrors != second.IndexErrors {
		t.Errorf("incremental: last-run index_errors=%d, want %d", lr.IndexErrors, second.IndexErrors)
	}
}
