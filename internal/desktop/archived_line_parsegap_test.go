package desktop

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
)

// covers: MA-348, MA-388, R1, X10
// F12 of the MF-S2 friction review (cell OW-7). The CLI and mailarchive-gui print how many messages carry
// a parse gap in the run result; the desktop's capture result line said only "N exported, M indexed" and
// left them to the Status & health card, though the result line is the first thing read (X10: one
// truth). It now adds the count and where to read more when it is not zero, in words true of every
// parse gap (a part kept as received or cut short, a text shown as read by an earlier version), and
// leaves the line exactly as it was when there is none.
func TestArchivedLineSaysHowManyMessagesCarryAParseGap(t *testing.T) {
	plain := archivedLine(app.Result{Stats: export.Stats{Exported: 3}, ManifestSize: 3, Indexed: 3})
	if plain != "Archived — 3 message(s) exported, 3 indexed." {
		t.Fatalf("positive twin: the line with no parse gap changed: %q", plain)
	}
	got := archivedLine(app.Result{Stats: export.Stats{Exported: 3}, ManifestSize: 3, Indexed: 3, Undecodable: 2, FidelityFallback: 1})
	for _, want := range []string{"3 message(s) exported, 3 indexed.", "2 message(s)", "a part kept as received or cut short", "read by an earlier version", "attachments-report.tsv"} {
		if !strings.Contains(got, want) {
			t.Errorf("the line lacks %q: %q", want, got)
		}
	}
	// It must not claim what is not true of every parse gap: a fallback or an unsplit message has no zip.
	for _, bad := range []string{"attachments zip", "cannot decode", "nothing is lost"} {
		if strings.Contains(got, bad) {
			t.Errorf("the line claims %q, which is not true of every parse gap: %q", bad, got)
		}
	}
}
