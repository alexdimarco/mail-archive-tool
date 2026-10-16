package main

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
)

// covers: MA-348, R1, R18, X2, X6
// F3 of the MF-S2 friction review, re-verification (cells OW-3, OW-11, CA-2): in a MIX of a message the
// source has not delivered and a message cut short in the mailbox file, the run summary kept only the
// download remedy and never said what cures the cut one. It keeps the download line for the first and
// says, for the others, that a download does not change a mailbox file and a complete export does.
// The positive twins: with no parse gap, and with only parse gaps, the lines are the ones they were.
func TestRunSummaryOfAMixSaysWhatCuresTheCutOnes(t *testing.T) {
	base := app.Result{Stats: export.Stats{Exported: 4}, ManifestSize: 4, Indexed: 4}

	plain := base
	plain.Fillable = 2
	if out := summaryText(plain); strings.Contains(out, "complete export") || strings.Contains(out, "in the mailbox file") {
		t.Fatalf("positive twin: an ordinary missing-content run mentions the mailbox file:\n%s", out)
	}

	mixed := base
	mixed.Fillable, mixed.Undecodable, mixed.FillableParseGap, mixed.FillableOnlyParseGap = 3, 1, 1, 1
	out := summaryText(mixed)
	if !strings.Contains(out, "download for offline use") {
		t.Errorf("a mix lost the download remedy:\n%s", out)
	}
	for _, want := range []string{"1 of them is cut short, or has parts that could not be told apart, in the mailbox file", "a download does not change it", "complete export"} {
		if !strings.Contains(out, want) {
			t.Errorf("a mix's summary never says what cures the cut message; lacks %q:\n%s", want, out)
		}
	}
}
