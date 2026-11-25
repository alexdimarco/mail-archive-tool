package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1499
// What MailArchive Desktop's Make a new archive… hears back after One archive for each mailbox
// (docs/design-per-mailbox-archives.md §10, revision 2's C8 and C11): AddArchives gives every archive
// the set made or continued, in the plan's order, each with its outcome and the set's folder
// (Made.Parent), so the Desktop can list each one and name the folder. Positive twin first: a set
// that ended well comes back as it was, with no error. Closing a later window after the archives
// were made (the summary or the schedule question) is not an error; any other error is passed on
// with the archives, which the Desktop still lists. Backing out before any folder was made is
// "nothing added" (no Made, no error); an error before any folder is passed on with none. makeSet,
// which the flow runs, gives each archive the set's folder. AddArchives starts with
// dialogfront.Arm() (R39) and hands the flow the hook that receives the set.
func TestAddArchivesCarriesEverySetArchiveWithItsOutcome(t *testing.T) {
	boom := errors.New("the schedule question could not be shown")
	made := []Made{
		{Folder: "/p/a", Name: "a", New: true, Outcome: MadeFinished, Schedule: ScheduleInstalled, Parent: "/p"},
		{Folder: "/p/b", Name: "b", Outcome: MadeStopped, Reason: setCancelled, Parent: "/p"},
		{Name: "c", Outcome: MadeNotStarted, Parent: "/p"},
	}
	if got, err := addSetOutcome(made, nil); err != nil || !reflect.DeepEqual(got, made) {
		t.Fatalf("positive twin: %+v %v", got, err)
	}
	if got, err := addSetOutcome(made, zenity.ErrCanceled); err != nil || !reflect.DeepEqual(got, made) {
		t.Errorf("a later window closed: %+v %v", got, err)
	}
	if got, err := addSetOutcome(made, boom); !errors.Is(err, boom) || !reflect.DeepEqual(got, made) {
		t.Errorf("an error after the archives: %+v %v", got, err)
	}
	if got, err := addSetOutcome(nil, zenity.ErrCanceled); err != nil || got != nil {
		t.Errorf("backed out before any folder: %+v %v", got, err)
	}
	if got, err := addSetOutcome(nil, boom); !errors.Is(err, boom) || got != nil {
		t.Errorf("an error before any folder: %+v %v", got, err)
	}

	// makeSet gives each archive the set's folder, whatever became of it.
	ownSettings(t)
	rows, _ := setMail(t)
	parent := filepath.Join(t.TempDir(), "Mail archives")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	s := &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled, cancel: map[int]bool{3: true}}
	s.install(t)
	got, err := makeSet(rows, "darwin")
	if err != nil || len(assure.Reached(t, got, "the set's archives")) != 5 {
		t.Fatalf("the set: %+v %v", got, err)
	}
	for i, m := range got {
		if m.Parent != parent {
			t.Errorf("archive %d (%s, outcome %d) names the folder %q, want %q", i, m.Name, m.Outcome, m.Parent, parent)
		}
	}
	if got[0].Outcome != MadeFinished || got[2].Outcome != MadeStopped || got[3].Outcome != MadeNotStarted {
		t.Errorf("the outcomes: %+v", got)
	}

	// The wiring: Arm first, the set's hook handed to the flow, its answer through addSetOutcome.
	src, err := os.ReadFile("wizard.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	i := strings.Index(body, "func AddArchives() ([]Made, error) {\n\tdialogfront.Arm()")
	if i < 0 {
		t.Fatal("AddArchives does not start with dialogfront.Arm()")
	}
	body = body[i:]
	body = body[:strings.Index(body, "\n}\n")]
	for _, w := range []string{
		"localArchiveFlowHooks(func(o string) { out = o }, func() { finished = true }, nil, nil, func(m []Made) { set, isSet = m, true })",
		"if isSet {\n\t\treturn addSetOutcome(set, err)\n\t}\n\treturn addOutcome(out, finished, err)",
	} {
		if !strings.Contains(body, w) {
			t.Errorf("AddArchives no longer holds %q", w)
		}
	}
}
