package wizard

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
)

// covers: MA-348, R1, R18, X6
// The GUI summary splits "Empty at the source" from the messages with parts this version
// cannot decode (design-mime-fidelity §4.9): the latter are named in plain words, with
// where the bytes are and where the details are, and are not counted as empty. A message
// kept as an earlier version read it has its own line. A clean run mentions neither.
func TestGUISummarySplitsEmptyFromCannotDecode(t *testing.T) {
	clean := exportSummary("/archive", false, app.Result{Stats: export.Stats{Exported: 1}, ReportPath: "/archive/attachments-report.tsv"})
	if strings.Contains(clean, "cannot decode") || strings.Contains(clean, "earlier version") {
		t.Fatalf("a clean run mentions parts it cannot decode:\n%s", clean)
	}

	got := exportSummary("/archive", false, app.Result{
		Stats: export.Stats{Exported: 5}, Terminal: 2, Undecodable: 3, FidelityFallback: 1,
		ReportPath: "/archive/attachments-report.tsv",
	})
	for _, want := range []string{
		"Empty at the source (nothing to download): 2",
		"Kept as received, because this version cannot decode a part of them: 3",
		"Kept as an earlier version read them, because this version could not decode them in full: 1",
		"Details: /archive/attachments-report.tsv",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}

	// Only undecodable messages: the details line is still offered.
	only := exportSummary("/archive", false, app.Result{Stats: export.Stats{Exported: 1}, Undecodable: 1, ReportPath: "/archive/attachments-report.tsv"})
	if !strings.Contains(only, "Details: /archive/attachments-report.tsv") || strings.Contains(only, "Empty at the source") {
		t.Errorf("a run with only a part this version cannot decode:\n%s", only)
	}
}
