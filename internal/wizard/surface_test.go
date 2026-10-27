package wizard

import (
	"errors"
	"testing"

	"github.com/ncruces/zenity"
)

// covers: MA-651, R33
// The wizard's new first step routes the "open MailArchive Desktop" pick to the
// in-process hand-off and everything else — the local item, the default, an empty
// pick — into the local-file wizard. Pure, so the branch is tabled without a dialog.
func TestSurfaceChoiceRoutes(t *testing.T) {
	if !surfaceOpensDesktop(surfaceDesktopItem) {
		t.Errorf("the Desktop item %q must open MailArchive Desktop", surfaceDesktopItem)
	}
	for _, local := range []string{surfaceLocalItem, "", "something else entirely"} {
		if surfaceOpensDesktop(local) {
			t.Errorf("%q must continue into the local-file wizard, not open Desktop", local)
		}
	}
}

// covers: MA-805, MA-651, R33
// The first step offers to open an archive someone already has, encrypted or not
// (design-encrypted-archives-phase3 §7.1): it opens MailArchive Desktop — where the
// folder is chosen and an encrypted one unlocked — never a password dialog here; the
// local item stays first, the default.
func TestTheFirstStepOpensAnArchiveInDesktop(t *testing.T) {
	if surfaceChoices[0] != surfaceLocalItem || !surfaceOpensDesktop(surfaceOpenItem) {
		t.Fatalf("choices %v", surfaceChoices)
	}
	found := false
	for _, c := range surfaceChoices {
		found = found || c == surfaceOpenItem
	}
	if !found {
		t.Fatal("the open item is not offered")
	}
}

// covers: MA-652, R14
// Removing a scheduled backup from the wizard's health view confirms first;
// removeConfirmed maps the dialog answer: OK removes, Cancel or a dismissed dialog
// keeps the backup, any other error propagates. Pure, mirroring repairFollowUp.
func TestRemoveConfirmed(t *testing.T) {
	if proceed, err := removeConfirmed(nil); !proceed || err != nil {
		t.Errorf("OK should remove: proceed=%v err=%v", proceed, err)
	}
	if proceed, err := removeConfirmed(zenity.ErrCanceled); proceed || err != nil {
		t.Errorf("Cancel/dismiss should keep the backup: proceed=%v err=%v", proceed, err)
	}
	boom := errors.New("boom")
	if proceed, err := removeConfirmed(boom); proceed || err == nil {
		t.Errorf("an unexpected dialog error should propagate: proceed=%v err=%v", proceed, err)
	}
}
