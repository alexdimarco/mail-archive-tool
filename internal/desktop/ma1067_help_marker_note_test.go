package desktop

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
)

// covers: MA-1067, R36
// The 1.5.6 friction review's note F (docs/review-delete-friction-1.5.6.md): the guide (How to use
// MailArchive) now says what the note a delete leaves in the folder is, and what removing it by hand
// does (docs/design-archive-delete.md §15.8 question 1's recommendation, kept by the owner's Q1
// ruling as "finish it, or remove the marker by hand"), and that Delete… on a row whose delete was
// finished on another computer asks "It was finished elsewhere" (§15.8 question 2, the owner's Q4
// ruling, MA-1057). On every platform, naming only controls the page draws: the question's two
// buttons are archdelete's own labels. Lead's words, owner-delegated 2026-11-02. Positive twin first:
// the guide's paragraph on a delete begun on another computer is still there.
func TestMA1067_TheGuideSaysWhatTheNoteIsAndAsksAboutAFinishElsewhere(t *testing.T) {
	if archdelete.ElsewhereYes != "It was finished elsewhere" || archdelete.ElsewhereNo != "Cancel" {
		t.Fatalf("the guide names buttons the question does not draw: %q / %q", archdelete.ElsewhereYes, archdelete.ElsewhereNo)
	}
	wants := []string{
		"The note in the folder: once a delete starts removing an archive's files, it leaves a note in the archive's folder, .mailarchive-deleting.json, until the delete is finished. While it is there, MailArchive does not back up into that folder, on this computer or on another computer or user account that runs this version or a later one. MailArchive removes the note only by finishing the delete. Removing it by hand lets backups write into the folder again, into an archive the delete may already have half emptied.",
		"A delete finished on another computer: if you began a delete here and it was finished from another computer or user account, this computer's row still says Delete not finished. Click Delete… on it: when the folder is not there, MailArchive asks whether the delete was finished elsewhere. It was finished elsewhere stops this computer's scheduled backups for the archive and removes its row (for an encrypted archive, this computer also forgets its record and key of it); then type the folder's name, as for any delete. If the folder is on a drive that is not plugged in, click Cancel and plug the drive in instead.",
	}
	for _, p := range helpDeletePlatforms {
		page := getOverview(t, Config{GOOS: p.goos, ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")})
		h := readHelpCard(t, page)
		assure.Reached(t, h.text, p.goos+": the guide's text")
		if !strings.Contains(h.text, "A delete begun on another computer: a shared folder or a drive can hold an archive") {
			t.Fatalf("%s: positive twin: the guide's paragraph on a delete begun on another computer", p.goos)
		}
		for _, want := range wants {
			if !strings.Contains(h.text, want) {
				t.Errorf("%s: the guide lacks %q", p.goos, want)
			}
		}
	}
}
