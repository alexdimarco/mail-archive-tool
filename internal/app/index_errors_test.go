package app

import (
	"context"
	"database/sql"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
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
// Every message a run exports has its row added or replaced in the search
// index, or is counted as an index error (F49), and the count lands in the
// last-run record. One message whose index insert fails costs exactly that one
// row — the other 49 of its batch stay indexed — and when SQLite abandons the
// whole transaction (RAISE(ROLLBACK), the shape of a disk-full/I-O failure)
// the earlier rows lost with it are counted too, never silently dropped.
// Result.Indexed is the index's total, so on a run that exports each key once,
// none of them already indexed, (Indexed after − Indexed before) +
// IndexErrors == exported; on a fresh archive that is Indexed + IndexErrors ==
// exported, which the ABORT and ROLLBACK runs below check. A message exported
// again under the same key has its row replaced, not added: a fill (as for a
// re-capture of a legacy "unknown" record or a full-mode re-export), and, on
// a fresh archive too, a key the same run exports twice
// (TestFreshRunIndexesAKeyExportedTwiceOnce). Neither equation holds for it:
// after a run that fills, every key it exported is in the index, its row
// carrying what that run exported.
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
	// Result.Indexed is the index's total (what status shows), and every
	// message this run exports is new to the index, so the run's own books are
	// the growth of the index plus its IndexErrors.
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

	// Fill: a healthy incremental run that fills gaps exports those messages
	// again under their keys (the fingerprint leaves bodies out). Two bodies
	// arrive: "fill old" was indexed, empty, by the first run, so its row is
	// replaced and the index does not grow for it; "fill new" was refused by
	// the index then (counted in that run's IndexErrors), so its row is added.
	// The growth equation therefore does not hold (the index grows by 1 while
	// the run exports 2), and the books are checked by key instead: every key
	// the run exported is in the index, its row carrying the body that run
	// exported, or the run counted it in IndexErrors.
	fillSrc := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, fillSrc, 3)
	gap := func(name, body string) {
		t.Helper()
		msg := "From: a@example.com\r\nSubject: fill " + name + "\r\nDate: Mon, 03 Mar 2025 10:00:00 +0000\r\nMessage-ID: <fill-" + name + "@x>\r\n\r\n" + body
		if err := os.WriteFile(filepath.Join(fillSrc, "cur", "fill-"+name+".eml"), []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gap("old", "") // no body yet: a fillable "body" gap
	gap("new", "")
	fillOut := tmpDir(t)
	fillOpts := Options{Inputs: []string{fillSrc}, Out: fillOut, Mode: export.Incremental, Index: true}
	injectIndexFault(t, fillOut, "fill new", "ABORT")
	gapRun, err := Run(context.Background(), fillOpts, logger, nil)
	if err != nil || gapRun.Stats.Exported != 5 || gapRun.Fillable != 2 || gapRun.IndexErrors != 1 {
		t.Fatalf("fill: first run exported=%d fillable=%d indexErrors=%d err=%v, want 5/2/1", gapRun.Stats.Exported, gapRun.Fillable, gapRun.IndexErrors, err)
	}
	clearIndexFault(t, fillOut)
	before := manifestRecords(t, fillOut)
	gap("old", "this body arrived later\r\n")
	gap("new", "this body arrived later\r\n")
	fillRun, err := Run(context.Background(), fillOpts, logger, nil)
	if err != nil {
		t.Fatalf("fill: second run failed: %v", err)
	}
	if fillRun.Stats.Exported != 2 || fillRun.Stats.Filled != 2 || fillRun.IndexErrors != 0 {
		t.Fatalf("fill: second run exported=%d filled=%d indexErrors=%d, want 2/2/0", fillRun.Stats.Exported, fillRun.Stats.Filled, fillRun.IndexErrors)
	}
	after := manifestRecords(t, fillOut)
	var exportedKeys []string
	for k, rec := range after {
		if prev, ok := before[k]; !ok || !prev.ExportedAt.Equal(rec.ExportedAt) {
			exportedKeys = append(exportedKeys, k)
		}
	}
	assure.Reached(t, exportedKeys, "keys the fill run exported")
	rows, carrying := indexedRows(t, fillOut, "arrived")
	for _, k := range exportedKeys {
		path := after[k].Path
		switch {
		case !rows[k]:
			t.Errorf("fill: %s was exported by the run but has no row in the index, and the run counted no index error (IndexErrors=%d)", path, fillRun.IndexErrors)
		case !carrying[path]:
			t.Errorf("fill: the index row of %s still carries an earlier run's content — the run exported it, but its row was not replaced", path)
		}
	}
	if grew := fillRun.Indexed - gapRun.Indexed; grew != 1 {
		t.Errorf("fill: the index grew by %d, want 1 — the row the index held is replaced, never doubled, and the row it lacked is added", grew)
	}
}

// covers: MA-388, R8, S29
// On a fresh archive the index books are the growth equation counted from an
// empty index, so Indexed + IndexErrors == exported holds only when the run
// exports each key once. One run can export a key twice: full mode never
// skips a key it has already seen, so a source holding a message twice is
// exported twice under one key, and an incremental run fills, in the same
// run, the gap an earlier copy of a message left. Either way the key's second
// export replaces its row — never a second row, never a refused insert — so
// the run exports 4 messages of 3 keys and the index holds 3 rows with no
// index error, every key the run exported is in the index, and the row
// carries the copy the run exported last.
func TestFreshRunIndexesAKeyExportedTwiceOnce(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	held := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, held, 3)
	raw, err := os.ReadFile(filepath.Join(held, "cur", "0.eml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(held, "cur", "0-copy.eml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	inRun := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, inRun, 2)
	twice := func(name, body string) { // maildir files are read in name order
		t.Helper()
		msg := "From: a@example.com\r\nSubject: twice\r\nDate: Mon, 03 Mar 2025 11:00:00 +0000\r\nMessage-ID: <twice@x>\r\n\r\n" + body
		if err := os.WriteFile(filepath.Join(inRun, "cur", name), []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	twice("twice-a.eml", "")                       // no body: a fillable gap
	twice("twice-b.eml", "the latecomer body\r\n") // fills it in the same run
	for _, tc := range []struct {
		name   string
		src    string
		mode   export.Mode
		filled int
		mid    string // the Message-ID exported twice
		last   string // a term only the copy exported last carries ("": the two copies are the same bytes)
	}{
		{"full mode over a message held twice", held, export.Full, 0, "m0@x", ""},
		{"an in-run fill", inRun, export.Incremental, 1, "twice@x", "latecomer"},
	} {
		out := tmpDir(t)
		res, err := Run(context.Background(), Options{Inputs: []string{tc.src}, Out: out, Mode: tc.mode, Index: true}, logger, nil)
		if err != nil {
			t.Fatalf("fresh, %s: run failed: %v", tc.name, err)
		}
		if res.Stats.Exported != 4 || res.ManifestSize != 3 || res.Stats.Filled != tc.filled {
			t.Fatalf("fresh, %s: exported=%d keys=%d filled=%d, want 4 exports of 3 keys, %d filled", tc.name, res.Stats.Exported, res.ManifestSize, res.Stats.Filled, tc.filled)
		}
		if res.Indexed != 3 || res.IndexErrors != 0 {
			t.Errorf("fresh, %s: indexed=%d indexErrors=%d, want 3/0 — the key exported twice keeps one row, replaced by its second export, never doubled and never refused", tc.name, res.Indexed, res.IndexErrors)
		}
		recs := manifestRecords(t, out)
		k := assure.Reached(t, keyOf(recs, tc.mid), "the manifest key of "+tc.mid+", exported twice")
		rows, _ := indexedRows(t, out, "body")
		for key, rec := range recs {
			if !rows[key] {
				t.Errorf("fresh, %s: %s was exported but has no row in the index (IndexErrors=%d)", tc.name, rec.Path, res.IndexErrors)
			}
		}
		if tc.last != "" {
			if _, carrying := indexedRows(t, out, tc.last); !carrying[recs[k].Path] {
				t.Errorf("fresh, %s: the index row of %s still carries the first copy, not the copy the run exported last", tc.name, recs[k].Path)
			}
		}
	}
}

// keyOf returns the manifest key that carries the given Message-ID, or "".
func keyOf(recs map[string]state.Record, mid string) string {
	for k := range recs {
		if strings.Contains(k, mid) {
			return k
		}
	}
	return ""
}

// clearIndexFault drops the trigger injectIndexFault planted, so the next run
// indexes every message.
func clearIndexFault(t *testing.T, out string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP TRIGGER inject_fault`); err != nil {
		t.Fatal(err)
	}
}

// manifestRecords returns the records of the manifest on disk in out.
func manifestRecords(t *testing.T, out string) map[string]state.Record {
	t.Helper()
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	return m.All()
}

// indexedRows reads out's search index as status does (read-only): the set of
// keys that have a row, and the set of archived paths whose indexed text
// matches term.
func indexedRows(t *testing.T, out, term string) (keys, matching map[string]bool) {
	t.Helper()
	ix, err := index.OpenReadonly(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	keys = map[string]bool{}
	if err := ix.EachRow(func(_ int64, key, _ string) error {
		keys[key] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	hits, _, err := ix.Search(index.Query{Text: term, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	matching = map[string]bool{}
	for _, h := range hits {
		matching[h.Path] = true
	}
	return keys, matching
}
