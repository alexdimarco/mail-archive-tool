package main

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
)

// covers: MA-348, R1, R18, X2, X6
// F3 of the MF-S2 friction review (cells OW-3, OW-11, CA-2). The run summary prints the remedy of a
// message a source has not delivered ("isn't cached locally (IMAP / a limited Outlook offline
// window) ... download for offline use") under "still missing content". When every message still
// missing content is only so because a part is cut short or a multipart has no boundary in the file
// it was read from, that remedy is for another source: a download returns the same bytes. The summary
// then says what cures those, and says once that they are also in the cannot-decode count. A mix keeps
// the download remedy and names the overlap. The positive twin: with no parse gap the lines are the
// ones they always were.
func TestRunSummaryRemedyFitsWhatIsMissing(t *testing.T) {
	const download = "download for offline use"
	base := app.Result{Stats: export.Stats{Exported: 4}, ManifestSize: 4, Indexed: 4}

	// Positive twin: an ordinary fillable gap keeps the download remedy and says nothing of an overlap.
	plain := base
	plain.Fillable = 2
	out := summaryText(plain)
	if !strings.Contains(out, download) || strings.Contains(out, "also counted") {
		t.Fatalf("an ordinary missing-content run changed:\n%s", out)
	}

	// Every missing message is a parse gap: the remedy is the complete export, not a download.
	onlyGaps := base
	onlyGaps.Fillable, onlyGaps.Undecodable, onlyGaps.FillableParseGap, onlyGaps.FillableOnlyParseGap = 2, 2, 2, 2
	out = summaryText(onlyGaps)
	if strings.Contains(out, download) || strings.Contains(out, "isn't cached locally") {
		t.Errorf("a cut or boundary-less message read in full is told to download for offline use:\n%s", out)
	}
	for _, want := range []string{"cut short", "replace", "complete export", "2 of them are also counted", "malformed when it was sent cannot be completed by either, and stays listed"} {
		if !strings.Contains(out, want) {
			t.Errorf("the summary lacks %q:\n%s", want, out)
		}
	}

	// A mix: the download remedy stays for the messages it is for, and the overlap is named.
	mixed := base
	mixed.Fillable, mixed.Undecodable, mixed.FillableParseGap, mixed.FillableOnlyParseGap = 3, 1, 1, 1
	out = summaryText(mixed)
	if !strings.Contains(out, download) || !strings.Contains(out, "1 of them is also counted") {
		t.Errorf("a mixed run lost the download remedy or the overlap note:\n%s", out)
	}
}
