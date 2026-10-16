package app

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// FI-S1a friction re-verification (docs/review-fi-s1a-friction.md, "Re-verification"):
// the fresh-eyes check's failed results and new findings, each with the actor's view.

// covers: MA-328, R18, S48
// OP-5 (the .ost twin of F1): an .ost that cannot be opened for a cause a route
// change cannot cure (permissions, another program holding it, a missing path, a disk
// error) is told that cause's remedy, not "a live Exchange .ost often cannot be read
// directly; run -outlook / graph". A parse or format error keeps the route advice.
func TestOSTOpenRemedyFollowsTheCause(t *testing.T) {
	route := func(s string) bool { return strings.Contains(s, "often cannot be read directly") }
	if got := dataFileRemedyFor("a.ost", errors.New("unsupported format type")); !route(got) {
		t.Fatalf("positive twin: a format error lost the route advice: %q", got)
	}
	for name, err := range map[string]error{
		"permission denied": &fs.PathError{Op: "open", Path: "a.ost", Err: fs.ErrPermission},
		"errno text":        errors.New("open a.ost: permission denied"),
		"in use on windows": errors.New("open a.ost: The process cannot access the file because it is being used by another process."),
		"missing":           errors.New("open a.ost: no such file or directory"),
		"disk error":        errors.New("read a.ost: input/output error"),
	} {
		for _, f := range []func(string, error) string{dataFileRemedyFor, shortFileRemedy} {
			got := f("a.ost", err)
			if route(got) || got == "" {
				t.Errorf("%s: %q must say the cause's remedy, not the route advice", name, got)
			}
		}
	}
	if got := dataFileRemedyFor("a.ost", errors.New("open a.ost: permission denied")); !strings.Contains(got, "permission") {
		t.Errorf("a locked .ost: %q does not name the permissions", got)
	}
}

// covers: MA-327, R1, R7, R19, S48
// IN-1 / F10 / R5: the root banner and README.txt promise a why and a what-to-do
// for every row, record rows included (ESC-16, §13.11); the banner shows on an
// archive that has only record rows (an unparsed message, an empty attachment, a
// missing body), and those rows now carry the why and the remedy.
func TestBannerAndReadmeOnlyPromiseWhatRowsCarry(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true}, logger, nil); err != nil {
		t.Fatal(err)
	}
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.Add("rec-1", state.Record{Path: "s/a.html", Folder: "Inbox", Subject: "A", Terminal: []string{"unparsed"}})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Reindex(out, logger); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := strings.Join(strings.Fields(string(b)), " ")
	if !strings.Contains(page, "attachments-report.tsv") {
		t.Fatalf("positive twin: a record-only archive has no banner:\n%s", page)
	}
	// ESC-16 (§13.11): a record row now carries a why and a what-to-do too, so the
	// promise is again for every row.
	if strings.Contains(page, "a row for a message or folder that left no page") {
		t.Error("the banner still narrows the promise to ledger rows")
	}
	if !strings.Contains(page, "each row says why it is missing and what to do about it") {
		t.Error("the banner does not promise a why and a remedy for every row")
	}
	readme := strings.Join(strings.Fields(archiveReadme), " ")
	if strings.Contains(readme, "row for a message or folder that left no page") {
		t.Error("README.txt still narrows the promise to ledger rows")
	}
	if !strings.Contains(readme, "The end of each row's detail says why it happened and what to do about it") {
		t.Error("README.txt does not promise a why and a remedy for every row")
	}
}

// covers: MA-327, MA-328, R18, S48
// R3: in the report there is no "see the report" to go to. An overflow row and a
// row of a reason this version does not know say something else.
func TestReportRowsNeverSendTheReaderBackToTheReport(t *testing.T) {
	out := tmpDir(t)
	m, _ := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	run := time.Now().UTC().Add(-time.Hour)
	const tok = "tok1"
	m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "x:1", Reason: "a-reason-from-the-future", Class: state.ClassTerminal, Subject: "S"}, run)
	m.CaptureGaps[state.GapKey(tok, state.GapScopeOverflow, "overflow:Zz")] = state.CaptureGap{Token: tok, Folder: "Zz", Scope: state.GapScopeOverflow,
		Locator: "overflow:Zz", Reason: "overflow", Class: state.ClassFillable, Skipped: 40, FirstRun: run, LastRun: run, Runs: 1}
	path, rows := writeReport(out, m, false, log.New(io.Discard, "", 0))
	if rows != 2 {
		t.Fatalf("%d report rows, want 2", rows)
	}
	for _, l := range readLines(t, path)[1:] {
		detail := strings.Split(l, "\t")[5]
		i := strings.Index(detail, "what to do: ")
		if i < 0 {
			t.Fatalf("a row without a remedy: %q", detail)
		}
		todo := detail[i:]
		if strings.Contains(todo, "see the report") {
			t.Errorf("the report's own row says to see the report: %q", todo)
		}
		if strings.TrimPrefix(todo, "what to do: ") == "" || !strings.Contains(todo, "run again") {
			t.Errorf("the row's remedy is empty or has no next step: %q", todo)
		}
	}
	// status and the run summary keep the pointer: there it is true.
	if got := state.GapRemedy("overflow", "", ""); !strings.Contains(got, "see the report") {
		t.Errorf("the shared remedy lost its pointer: %q", got)
	}
}

// covers: MA-323, MA-327, S46, S48
// OP-8: a bidirectional override in a store id (an input path or a mailbox address)
// or in a record row's subject or folder (mail text) must not reach the report or a
// status line's source name; the ledger's own fields were already stripped.
func TestReportStripsBidiFromRecordRowsAndSourceNames(t *testing.T) {
	const rlo = "\u202e"
	if got := state.SourceDisplay("path:/data/" + rlo + "exe.pst"); strings.Contains(got, rlo) || got != "/data/exe.pst" {
		t.Errorf("SourceDisplay kept a bidi control: %q", got)
	}
	out := tmpDir(t)
	m, _ := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	m.Add("k1", state.Record{Path: "s/a.html", Folder: "In" + rlo + "box", Subject: "inv" + rlo + "fdp.exe", Terminal: []string{"unparsed"}})
	m.Stores = map[string]string{"path:/data/" + rlo + "exe.pst": "tok9"}
	m.ObserveGap(state.CaptureGap{Token: "tok9", Folder: "Inbox", Scope: state.GapScopeItem, Locator: "x:1", Reason: "file-unreadable", Class: state.ClassTerminal}, time.Now().UTC().Add(-time.Hour))
	path, rows := writeReport(out, m, false, log.New(io.Discard, "", 0))
	if rows != 2 {
		t.Fatalf("%d report rows, want 2", rows)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), rlo) {
		t.Errorf("the report carries U+202E:\n%q", b)
	}
}

// covers: MA-327, R1, S48
// OP-7: the wording of verify's added lines. "retried whenever the archive runs
// again" read as past tense; and in a tampered archive the sentence about what
// "attested" means came before "NOT attested".
func TestVerifyGapLinesReadInTheRightTense(t *testing.T) {
	healthy := VerifySummary(Report{Out: "/a", Records: 1, GapItems: 2, GapRetryable: 1, GapNeedAction: 1})
	joined := strings.Join(healthy, "\n")
	if strings.Contains(joined, "retried whenever") {
		t.Errorf("past-tense wording remains:\n%s", joined)
	}
	if !strings.Contains(joined, "1 will be retried the next time the archive runs and 1 need action") {
		t.Errorf("the split does not read as a future retry:\n%s", joined)
	}
	if !strings.Contains(joined, "\"attested\" below means the files this archive holds are intact, not that the archive is complete") {
		t.Errorf("the healthy line lost its sentence:\n%s", joined)
	}
	tampered := VerifySummary(Report{Out: "/a", Records: 1, Modified: 1, GapItems: 1, GapRetryable: 1})
	for i, l := range tampered {
		if strings.Contains(l, "\"attested\" below") {
			t.Errorf("a tampered archive's line %d promises an attested verdict below: %q", i, l)
		}
	}
	joined = strings.Join(tampered, "\n")
	if !strings.Contains(joined, "NOT attested") || !strings.Contains(joined, "not that the archive is complete") {
		t.Errorf("a tampered archive's summary lacks the verdict or the completeness sentence:\n%s", joined)
	}
}
