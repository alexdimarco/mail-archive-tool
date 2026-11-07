package tbrestore

import (
	"fmt"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1122, R40, S68
// W-LEFT's "entries it did not make" quote each entry the way the plan's NAMES list does
// (settled 2026-11-07, the lead's call): an entry named "100%", one whose name holds a line
// break or a bell shows quoted, its control characters escaped, so a name cannot pass for a
// line of the result or for a percentage. Both forms (nothing removed; part removed). An
// encrypted archive's words name no entry.
func TestTheLeftoverEntriesAreQuoted(t *testing.T) {
	foreign := []string{"100%", "Inbox.quarantined", "a\nb\a"}
	want := `("100%", "Inbox.quarantined", "a\nb\a")`
	for _, c := range []CleanResult{
		{Kind: CleanLeft, Foreign: foreign, ForeignN: 3},
		{Kind: CleanLeftPart, Foreign: foreign, ForeignN: 3},
	} {
		got := leftoverWords(c, "Restored — Work", "/p/Mail/Local Folders", false)
		assure.Reached(t, strings.Contains(got, want), fmt.Sprintf("kind %v: each entry quoted, control characters escaped:\n got %q\nwant it to hold %q", c.Kind, got, want))
		assure.Reached(t, !strings.ContainsAny(got, "\n\a"), fmt.Sprintf("kind %v: no raw control character from a name: %q", c.Kind, got))

		enc := leftoverWords(c, "Restored — Work", "/p/Mail/Local Folders", true)
		assure.Reached(t, enc != "" && !strings.Contains(enc, "100%") && !strings.Contains(enc, "quarantined") && !strings.Contains(enc, `"`),
			fmt.Sprintf("kind %v: an encrypted archive's words name no entry: %q", c.Kind, enc))
	}
	one := leftoverWords(CleanResult{Kind: CleanLeft, Foreign: []string{"x"}, ForeignN: 1}, "T", "P", false)
	assure.Reached(t, strings.Contains(one, `found 1 entry it did not make in the folder 'T.sbd' ("x")`), "one entry: "+one)
}
