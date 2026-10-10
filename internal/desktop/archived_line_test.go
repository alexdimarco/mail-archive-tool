package desktop

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
)

// covers: MA-388, R8, S33
// The capture result reconciles exports with new messages (g7 friction F13):
// "4 exported, 3 indexed" over a message the source held twice now says one
// export replaced an earlier copy. Positive twins: every export new, and a fill
// (which "filled" explains), print the plain line.
func TestArchivedLineReconcilesDuplicates(t *testing.T) {
	for name, r := range map[string]app.Result{
		"all new": {Stats: export.Stats{Exported: 3}, ManifestSize: 3, Indexed: 3},
		"fill":    {Stats: export.Stats{Exported: 2, Filled: 2}, ManifestBefore: 4, ManifestSize: 4, Indexed: 4},
	} {
		if got := archivedLine(r); strings.Contains(got, "replaced") {
			t.Fatalf("%s: %q", name, got)
		}
	}
	got := archivedLine(app.Result{Stats: export.Stats{Exported: 4}, ManifestSize: 3, Indexed: 3})
	if !strings.Contains(got, "4 message(s) exported, 3 indexed") || !strings.Contains(got, "1 of the exports replaced an earlier copy") || !strings.Contains(got, "3 new") {
		t.Fatalf("the duplicate is not reconciled: %q", got)
	}
}
