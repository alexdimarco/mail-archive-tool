package main

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
)

// covers: MA-348, R1, R18, X6
// The run summary's Verification line counts the messages with parts this version cannot
// decode apart from the source-empty ones (design-mime-fidelity §4.9, X2): what (how
// many messages), why (they are kept as received) and where (see the report); a message
// kept as an earlier version read it has its own line. A run with neither prints
// nothing about them, and the line is still printed when they are the only thing to say.
func TestRunSummaryCountsMessagesThatCannotBeDecoded(t *testing.T) {
	// Positive twin: nothing undecodable, nothing said.
	if out := summaryText(app.Result{Stats: export.Stats{Exported: 3}, Terminal: 1, ManifestSize: 3, Indexed: 3}); strings.Contains(out, "cannot decode") || strings.Contains(out, "earlier version read") {
		t.Fatalf("a run with nothing undecodable mentions it:\n%s", out)
	}

	r := app.Result{Stats: export.Stats{Exported: 3}, Terminal: 1, Undecodable: 2, FidelityFallback: 1, ManifestSize: 3, Indexed: 3, ReportPath: "/tmp/archive/attachments-report.tsv"}
	out := summaryText(r)
	for _, want := range []string{
		"1 source-empty (never fillable)",
		"2 message(s) with parts this version cannot decode (kept as received; see the report)",
		"1 message(s) kept as an earlier version read them",
		"/tmp/archive/attachments-report.tsv",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the summary lacks %q:\n%s", want, out)
		}
	}
	// The cannot-decode count is its own: it is not folded into the source-empty one.
	if strings.Contains(out, "3 source-empty") {
		t.Errorf("parse gaps are counted as source-empty:\n%s", out)
	}

	// Only undecodable messages: the Verification line is still printed.
	only := summaryText(app.Result{Undecodable: 1, ManifestSize: 1, Indexed: 1})
	if !strings.Contains(only, "Verification:") || !strings.Contains(only, "1 message(s) with parts this version cannot decode") {
		t.Errorf("a run with only a part this version cannot decode prints no Verification line:\n%s", only)
	}
}
