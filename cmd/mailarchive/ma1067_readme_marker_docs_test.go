package main

import (
	"strings"
	"testing"
)

// covers: MA-1067, R36
// The 1.5.6 friction review's note F (docs/review-delete-friction-1.5.6.md): the README's "Deleting
// an archive" says what the note a delete leaves in the folder is and what removing it by hand does
// (docs/design-archive-delete.md §15.8 question 1's recommendation), and that Delete… — or
// delete-archive at the terminal — on a delete finished on another computer asks "It was finished
// elsewhere" (§15.8 question 2, MA-1057). The controls it names in bold are drawn in those words
// (TestTheDeleteDocsSayWhat153DoesAndNameRealControls). Lead's words, owner-delegated 2026-11-02.
// Positive twin first: the README's paragraph on a delete begun on another computer is still there.
func TestMA1067_TheReadmeSaysWhatTheNoteIsAndAsksAboutAFinishElsewhere(t *testing.T) {
	readme := strings.Join(strings.Fields(docSection(t, "README.md", "## Deleting an archive")), " ")
	if !strings.Contains(readme, "**A delete begun on another computer.** A shared folder or a drive can hold an archive") {
		t.Fatalf("positive twin: the README's paragraph on a delete begun on another computer:\n%s", readme)
	}
	for _, want := range []string{
		"**The note in the folder.** Once a delete starts removing an archive's files, it leaves a note in the archive's folder, `.mailarchive-deleting.json`, until the delete is finished. While it is there, MailArchive does not back up into that folder, on this computer or on another computer or user account that runs this version or a later one. MailArchive removes the note only by finishing the delete. Removing it by hand lets backups write into the folder again, into an archive the delete may already have half emptied.",
		"**A delete finished on another computer.** If you began a delete here and it was finished from another computer or user account, this computer's row still says Delete not finished. Click **Delete…** on it: when the folder is not there, MailArchive asks whether the delete was finished elsewhere. **It was finished elsewhere** stops this computer's scheduled backups for the archive and removes its row (for an encrypted archive, this computer also forgets its record and key of it); then type the folder's name, as for any delete. If the folder is on a drive that is not plugged in, click **Cancel** and plug the drive in instead. `mailarchive delete-archive -out DIR` asks the same question.",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("the README lacks %q", want)
		}
	}
}
