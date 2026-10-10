package main

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
)

// covers: MA-328, R18, R12, S48
// The run summary's "Not captured" block (§4.10): after Done., the counts of items
// (and how many are new this run) and folders, how many will be retried and how
// many need action, the report path, then the per-gap lines the run built and the
// "Recovered" line. A run that observed nothing prints neither.
func TestSummaryNotCapturedBlock(t *testing.T) {
	line1 := `  ann@contoso.com/Inbox: "Quarterly report" (2026-03-02) — Microsoft 365 returned an error (500 ErrorMimeContentConversionFailed); it is retried when you run again. If you are about to lose access to this mailbox, save a copy from Outlook on the web now.`
	line2 := `  ann@contoso.com/Archive/長いフォルダ名: the folder could not be written to /tmp/archive (file name too long) — rename the folder in your mail app, or archive to an NTFS/APFS drive`
	r := app.Result{
		Stats: export.Stats{Exported: 5}, ManifestSize: 5, ReportPath: "/tmp/archive/attachments-report.tsv",
		Gaps: app.GapSummary{Observed: 4, New: 1, Recovered: 4, Fillable: 2, Terminal: 2, ItemGaps: 3, FolderGaps: 1, Lines: []string{line1, line2}},
	}
	out := summaryText(r)
	want := []string{
		"Done. exported=5",
		"Not captured: 3 item(s) (1 new this run), 1 folder — 2 will be retried when you run again, 2 need action — details in /tmp/archive/attachments-report.tsv",
		line1, line2,
		"Recovered: 4 item(s) that failed on an earlier run were captured this run.",
	}
	at := -1
	for _, w := range want {
		i := strings.Index(out, w)
		if i < 0 {
			t.Fatalf("the summary lacks %q:\n%s", w, out)
		}
		if i < at {
			t.Errorf("%q is out of order:\n%s", w, out)
		}
		at = i
	}

	clean := summaryText(app.Result{Stats: export.Stats{Exported: 5}, ManifestSize: 5})
	for _, bad := range []string{"Not captured", "Recovered"} {
		if strings.Contains(clean, bad) {
			t.Errorf("a run that observed no gap printed %q:\n%s", bad, clean)
		}
	}
	// A run that only recovered says so, without a block.
	rec := summaryText(app.Result{Gaps: app.GapSummary{Recovered: 2}})
	if !strings.Contains(rec, "Recovered: 2 item(s)") || strings.Contains(rec, "Not captured") {
		t.Errorf("a recovery-only summary:\n%s", rec)
	}
	// Partly-read and unparsed records get their own Verification line, never the
	// "download for offline use" remedy of a missing body.
	pr := summaryText(app.Result{Unreadable: 2, Unparsed: 1})
	for _, w := range []string{"Verification:", "2 message(s) read only in part", "1 unparsed", "scanpst.exe"} {
		if !strings.Contains(pr, w) {
			t.Errorf("the partly-read Verification line lacks %q:\n%s", w, pr)
		}
	}
	if strings.Contains(pr, "download for offline use") {
		t.Errorf("a partly-read message got the missing-content remedy:\n%s", pr)
	}
}

// covers: MA-328, R18, R12, S48
// A partial run prints its summary (Done. and the block) and never a FAILED: line;
// a real failure still logs one. S1a maps a partial run to today's failure: the
// process exits 1 (X1 is not amended) with the one-line summary as its error.
func TestPartialRunPrintsDoneAndNeverFailed(t *testing.T) {
	partial := error(&app.PartialError{Summary: app.GapSummary{Observed: 1, ItemGaps: 1}})
	var buf bytes.Buffer
	logFailure(log.New(&buf, "", 0), partial)
	if buf.Len() != 0 {
		t.Errorf("a partial run logged %q; it must print its summary and no FAILED: line", buf.String())
	}
	logFailure(log.New(&buf, "", 0), errors.New("boom"))
	if buf.String() != "FAILED: boom\n" {
		t.Errorf("a real failure logged %q, want 'FAILED: boom'", buf.String())
	}
	if exitCode(partial) != 1 {
		t.Errorf("a partial run exits %d, want 1 (X1: today's failure)", exitCode(partial))
	}
	// The summary of a partial run is printed by the verbs: graph prints it for
	// every outcome, the export verb for nil, a handled interrupt and a partial run.
	if !printsSummary(nil) || !printsSummary(partial) || printsSummary(errors.New("boom")) {
		t.Error("printsSummary: a clean and a partial run print their summary, a failed one does not")
	}
}

// covers: MA-328, R18, R12, S48
// graph's outcome for a partial run is the run's error (exit 1, today's failure),
// and the handled interrupt stays a success.
func TestGraphOutcomeOfAPartialRun(t *testing.T) {
	partial := error(&app.PartialError{Summary: app.GapSummary{Observed: 2, ItemGaps: 2}})
	if got := graphOutcome(log.New(&bytes.Buffer{}, "", 0), partial); !errors.Is(got, app.ErrPartial) || exitCode(got) != 1 {
		t.Errorf("graphOutcome(partial) = %v (exit %d); want the partial error, exit 1", got, exitCode(got))
	}
}
