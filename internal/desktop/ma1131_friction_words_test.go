package desktop

import (
	"strings"
	"testing"
)

// covers: MA-1131, X11
// The words the R7 friction review settled (docs/review-restore-thunderbird-friction.md,
// 2026-11-07, Windows first on Edge; lead's words, owner-delegated):
//   - Save this record goes through the browser, which by default saves into Downloads without
//     asking, so the encrypted archive's note says where it lands, not "where you choose";
//   - the locked button's reason ends in one full stop, not "Thunderbird…." ;
//   - a folder's number in the tree counts its subfolders' messages too (Projects (6) held 5
//     and its subfolder 1), and the step line and the guide say so;
//   - the guide says where the record is saved, and what to do when a profile has no Local Folders.
func TestTheRestoreWordsTheFrictionReviewSettled(t *testing.T) {
	if strings.Contains(restoreRecordNote, "where you choose") ||
		!strings.Contains(restoreRecordNote, "The record names the restored folders and is not encrypted") ||
		!strings.Contains(restoreRecordNote, "your Downloads folder") {
		t.Errorf("the encrypted record's note says where the browser saves it: %q", restoreRecordNote)
	}
	if strings.Contains(restoreLockedWhy, "….") || !strings.Contains(restoreLockedWhy, "unlock it on its Encryption card") {
		t.Errorf("the locked button's reason: %q", restoreLockedWhy)
	}
	const treeLine = "The number beside each is how many messages in it and its subfolders can go back."
	if !strings.Contains(restoreTBJS, treeLine) || strings.Contains(restoreTBJS, "how many of its messages can go back") {
		t.Error("the tree's step line says a folder's number counts its subfolders")
	}
	guide := string(helpCard("windows", true, false, true))
	for _, want := range []string{
		"the number beside each is how many messages in it and its subfolders can go back",
		"Your browser saves it with your downloads",
		"no Local Folders",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("the guide's restore part lacks %q", want)
		}
	}
	if strings.Contains(guide, "how many of its messages can go back") {
		t.Error("the guide still says a folder's number is only its own messages")
	}
}
