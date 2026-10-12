package desktop

import "testing"

// The extraction workspace's slice E6, the desktop's half (design
// docs/design-extraction-workspace.md §4.1, §4.12): the Extract attachments toggle, its
// words and what it decides. Catalog-first: a skip encoder until the owner rules.

// covers: MA-424, MA-445, R12, S52
// The toggle decides the export: the workspace page offers "Extract attachments", OFF, the
// page's script sends it with the selection, the preview names it only when on, the start
// remembers it and the run writes the loose files. Held for the owner: TestExportSettingsRoundTrip
// (MA-445) pins the option hidden and a start with it set staying off, until slice E6 (rulings
// §0, rule 2).
func TestExtractAttachmentsToggleDecidesTheExport(t *testing.T) {
	t.Skip("MA-424: the workspace page hides the option and TestExportSettingsRoundTrip asserts that; the owner rules on that test before it changes")
}
