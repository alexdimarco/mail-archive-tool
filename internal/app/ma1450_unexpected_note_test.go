package app

import (
	"strings"
	"testing"
)

// covers: MA-1450, R5
// verify's note on `unexpected` files names, among what can leave them, the new message files a
// backup wrote into this folder in the seconds before it noticed its drive had been swapped (the
// 1.7.2 friction review's F8; MA-1328's guard leaves them, unlisted), so a person who swaps drives
// is not told only of an old binary or a hand-added file. Positive twin first: a summary with no
// unexpected file says nothing of them.
func TestTheUnexpectedNoteNamesASwappedDrivesFiles(t *testing.T) {
	if s := strings.Join(VerifySummary(Report{Out: "/a", Records: 1, Checked: 1, OK: 1}), "\n"); strings.Contains(s, "unexpected` files") {
		t.Fatalf("positive twin: a summary with no unexpected file has the note:\n%s", s)
	}
	want := "note: `unexpected` files are under a store directory but owned by no record — a leftover from an old-binary excursion, a file added by hand, or message files another archive's backup wrote here before it noticed its drive had been swapped; compare bytes before deleting anything."
	lines := VerifySummary(Report{Out: "/a", Records: 1, Checked: 1, OK: 1, Unexpected: 940})
	for _, l := range lines {
		if l == want {
			return
		}
	}
	t.Fatalf("the summary lacks the note %q:\n%s", want, strings.Join(lines, "\n"))
}
