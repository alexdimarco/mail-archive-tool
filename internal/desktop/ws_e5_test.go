package desktop

import "testing"

// The extraction workspace's slice E5, the desktop's half (design
// docs/design-extraction-workspace.md §4.1, §4.11, §4.17, §10): the Readable HTML toggle,
// its default and what it decides. It stays a skip encoder until the owner rules on the two
// tests whose assertions it changes.

// covers: MA-445, MA-423, R12, S52
func TestReadableHTMLToggleDecidesTheExport(t *testing.T) {
	t.Skip("MA-445: turning Readable HTML on (the plan accepts the option, the setting defaults ON, the page shows the toggle) changes what TestExportSettingsRoundTrip (MA-445) and TestCopyExportPlanIsADryRun (its R-h for the option) assert, which is the owner's call (rulings §0, rule 2); lands in EW-E5 once the owner rules")
}
