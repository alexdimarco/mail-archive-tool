package main

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
)

// covers: MA-348, R1, R18, X2, X6, X10
// F3 of the MF-S2 friction review, re-verification (cells OW-3, OW-7, OW-11, CA-2; surface: the
// mailarchive-gui finish summary, which the F3 fix of the CLI and `status` did not reach). When every
// message still missing content is only so because a part is cut short, or its parts could not be
// told apart, in the mailbox file it was read from, "open these in your mail app so it downloads
// them" is the remedy of a message a source has not delivered: a download returns the same bytes. The
// summary then says what cures them (a complete export of the file; for an IMAP cache a download),
// says that a message malformed when it was sent stays listed, and says once that they are also in
// the kept-as-received count. A mix keeps the download line and names what cures the cut ones. The
// positive twin: with no parse gap the lines are the ones they always were.
func TestExportSummaryRemedyFitsWhatIsMissing(t *testing.T) {
	const download = "open these in your mail app so it downloads them"
	base := app.Result{Stats: export.Stats{Exported: 4}, ManifestSize: 4, Indexed: 4, ReportPath: "/archive/attachments-report.tsv"}

	// Positive twin: an ordinary fillable gap keeps the download line and says nothing of an overlap.
	plain := base
	plain.Fillable = 2
	out := exportSummary("/archive", false, plain)
	if !strings.Contains(out, "Not fully downloaded yet: 2") || !strings.Contains(out, download) || strings.Contains(out, "also counted") {
		t.Fatalf("an ordinary missing-content run changed:\n%s", out)
	}

	// Every missing message is a parse gap: the remedy is the complete export, not a download.
	onlyGaps := base
	onlyGaps.Fillable, onlyGaps.Undecodable, onlyGaps.FillableParseGap, onlyGaps.FillableOnlyParseGap = 2, 2, 2, 2
	out = exportSummary("/archive", false, onlyGaps)
	if strings.Contains(out, download) || strings.Contains(out, "Not fully downloaded") {
		t.Errorf("a cut or boundary-less message read in full is told to download it in the mail app:\n%s", out)
	}
	for _, want := range []string{"Cut short", "complete export", "replace", "IMAP", "2 of them are also counted", "malformed when it was sent cannot be completed by either, and stays listed"} {
		if !strings.Contains(out, want) {
			t.Errorf("the summary lacks %q:\n%s", want, out)
		}
	}

	// A mix keeps the download line for the messages it is for, and says what cures the cut one.
	mixed := base
	mixed.Fillable, mixed.Undecodable, mixed.FillableParseGap, mixed.FillableOnlyParseGap = 2, 1, 1, 1
	out = exportSummary("/archive", false, mixed)
	if !strings.Contains(out, download) || !strings.Contains(out, "Not fully downloaded yet: 2") {
		t.Errorf("a mixed run lost the download line:\n%s", out)
	}
	for _, want := range []string{"1 of them is cut short", "complete export", "1 of them is also counted"} {
		if !strings.Contains(out, want) {
			t.Errorf("a mixed run's summary lacks %q:\n%s", want, out)
		}
	}
}
