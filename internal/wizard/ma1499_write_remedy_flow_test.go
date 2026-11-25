package wizard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ncruces/zenity"
)

// covers: MA-1499, MA-1496, S90
// Round B's leftover of the friction review's F2 (docs/design-per-mailbox-archives.md revision 3):
// when this computer does not let MailArchive make a folder in the set's parent (the parent made
// read-only after the plan, as Controlled folder access refuses one on Windows), the set stops and
// the archive's Made carries the summary's F2 remedy (Made.Remedy, setWriteRemedy), which MailArchive
// Desktop's note gives in place of "choose the same folder" (TestTheNoteForAParentItCouldNotWriteGivesItsRemedy).
// Positive twin first: a set made into a parent it can write carries no remedy. Unix only: Windows
// ignores the folder mode.
func TestASetRefusedByItsParentCarriesTheRemedy(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a read-only folder mode refuses no one here (Windows ignores it; root passes it)")
	}
	ownSettings(t)
	rows, _ := setMail(t)
	fresh := func() string {
		p := filepath.Join(t.TempDir(), "Mail archives")
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	run := func(parent string, lockAfterPlan bool) ([]Made, *flowScript) {
		t.Helper()
		(&setScheduler{}).install(t)
		s := &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
		s.install(t)
		if lockAfterPlan {
			inner := questionDialog
			questionDialog = func(text string, o ...zenity.Option) error {
				err := inner(text, o...)
				if strings.HasPrefix(text, "MailArchive will make") {
					if cerr := os.Chmod(parent, 0o500); cerr != nil {
						t.Fatal(cerr)
					}
				}
				return err
			}
			t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
		}
		made, _ := makeSet(rows, "darwin")
		return made, s
	}

	// Positive twin: a parent it can write.
	made, _ := run(fresh(), false)
	for _, m := range made {
		if m.Remedy != "" || m.Outcome != MadeFinished {
			t.Fatalf("positive twin: every archive finished, none with a remedy: %+v", made)
		}
	}

	parent := fresh()
	made, s := run(parent, true)
	if len(made) < 2 {
		t.Fatalf("the set's archives: %+v", made)
	}
	want := setWriteRemedy(runtime.GOOS)
	if made[0].Outcome != MadeFailed || made[0].Remedy != want || made[0].Reason != "this computer did not let MailArchive make a folder in "+parent {
		t.Errorf("the first archive is refused by the parent and carries F2's remedy: %+v", made[0])
	}
	for _, m := range made[1:] {
		if m.Outcome != MadeNotStarted {
			t.Errorf("the set stops: %+v", m)
		}
	}
	if sum := s.summaryOf(t); !strings.Contains(sum, want) || strings.Contains(sum, "The mail copied so far is kept") {
		t.Errorf("the summary gives the remedy:\n%s", sum)
	}
	if entries, _ := os.ReadDir(parent); len(entries) != 0 {
		t.Errorf("nothing was made in the parent: %v", entries)
	}
}
