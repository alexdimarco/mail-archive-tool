package tbengine

import (
	"fmt"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1124, MA-1128, R40, S72
// The plan's W-LONGPATH line quotes each folder it leaves out (settled 2026-11-07, the
// lead's call, owner-delegated words), as the changed-name list does: a name holding a
// line break or another control character shows it escaped inside its quotes (\n, \a),
// so it cannot pass for a line of the plan, and "100%" reads as a name. The line is what
// the terminal's -plan prints and what -record-out and Desktop's saved record keep (the
// plan's Text, checked through a real deep plan on Windows). An encrypted archive's line
// is still the count only.
func TestTheLongPathLineQuotesEachName(t *testing.T) {
	names := []string{"Reports/100%", "Reports/Line\nbreak", "Reports/bell\a"}
	got := longPathWords(names, false)
	want := `3 folders are nested too deep for Windows to hold under this profile, so they are not restored: "Reports/100%", "Reports/Line\nbreak", "Reports/bell\a".`
	assure.Reached(t, got == want, fmt.Sprintf("each name quoted, its control characters escaped:\n got %q\nwant %q", got, want))
	assure.Reached(t, !strings.ContainsAny(got, "\n\a"), fmt.Sprintf("no raw control character from a name reaches the plan: %q", got))

	enc := longPathWords(names, true)
	assure.Reached(t, enc == "3 folders are nested too deep for Windows to hold under this profile, so they are not restored.",
		fmt.Sprintf("an encrypted archive's line is the count only: %q", enc))
}
