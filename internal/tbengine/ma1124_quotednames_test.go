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

// covers: MA-1124, MA-1128, R40, S72
// W-FAT32 quotes the folder it names the same way (settled 2026-11-07, the lead's call): a
// folder "Reports/100%" or one whose name holds a line break or a bell shows quoted, its
// control characters escaped, so "100%" reads as a name and no break from a name reaches the
// refusal's words. An encrypted archive's words are the count only, naming no folder.
func TestTheFAT32RefusalQuotesTheFolder(t *testing.T) {
	for name, quoted := range map[string]string{
		"Reports/100%":        `"Reports/100%"`,
		"Reports/Line\nbreak": `"Reports/Line\nbreak"`,
		"Reports/bell\a":      `"Reports/bell\a"`,
	} {
		got := fat32Words([]string{name, "Other"}, 5<<30, false)
		assure.Reached(t, strings.HasPrefix(got, quoted+" is 5.0 GiB, too big for the drive that holds Local Folders"),
			fmt.Sprintf("the folder quoted, its control characters escaped: %q", got))
		assure.Reached(t, !strings.ContainsAny(got, "\n\a"), fmt.Sprintf("no raw control character from a name reaches the words: %q", got))
	}
	enc := fat32Words([]string{"Reports/100%", "Reports/Line\nbreak"}, 5<<30, true)
	assure.Reached(t, strings.HasPrefix(enc, "Of the folders chosen, 2 are 4 GB or more") && !strings.Contains(enc, "Reports"),
		fmt.Sprintf("an encrypted archive's words are the count only: %q", enc))
}
