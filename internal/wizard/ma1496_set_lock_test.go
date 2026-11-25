package wizard

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
)

// numbered lists the folders in parent whose name ends in a " (N)" number.
func numbered(t *testing.T, parent string) []string {
	t.Helper()
	ents, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if setNumberedRe.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}

// setNumberedRe is a folder name with a " (N)" number at its end.
var setNumberedRe = regexp.MustCompile(` \([0-9]+\)$`)

// covers: MA-1496, MA-1500, S90
// Two windows never make one mailbox's archive twice (the adversarial pass's CON-03; the lead's
// fold: "A lock beside the made-list (never in the parent) is held for the whole set run; a second
// window says another window is making archives there"). Two sets planned at once into one folder
// each made the other's archives again as " (2)" copies, each with its own schedule. makeSet now
// holds the set lock (job.HoldSetLock, wizard-set.lock in the settings folder) from before its folder
// question to its end. Positive twin first: one window after the other, the second brings every
// archive up to date and makes no " (2)". Then a second window started while the first exports its
// first archive is refused at once, in words that say another window is making archives and to try
// again when it has finished: it asks nothing, makes nothing in the folder and records nothing in the
// made-list; the first window finishes every archive; and once it has, a third window runs and makes
// no " (2)" either. The lock file is beside the made-list, never in the parent.
func TestTwoWindowsNeverMakeOneMailboxsArchiveTwice(t *testing.T) {
	ownSettings(t)
	rows, _ := setMail(t)
	(&setScheduler{}).install(t)
	run := func(parent string, onOpen func(int)) ([]Made, error) {
		t.Helper()
		s := &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled, onOpen: onOpen}
		s.install(t)
		return makeSet(rows, "darwin")
	}

	// Positive twin: one window after the other.
	p := setFresh(t)
	if _, err := run(p, nil); err != nil {
		t.Fatal(err)
	}
	made, err := run(p, nil)
	if err != nil || len(made) != 4 || len(numbered(t, p)) != 0 {
		t.Fatalf("positive twin: %+v %v, numbered %q", made, err, numbered(t, p))
	}
	for _, m := range made {
		if m.New || m.Outcome != MadeFinished {
			t.Errorf("positive twin: not brought up to date: %+v", m)
		}
	}

	// Two at once: a second window while the first exports its first archive.
	q := setFresh(t)
	var second []Made
	var secondErr error
	var asked bool
	var treeBefore, treeAfter []string
	var listBefore, listAfter []job.Made
	first := func(k int) {
		if k != 1 {
			return
		}
		treeBefore, listBefore = treeOf(t, q), madeLines(t)
		// The second window's own dialogs: any of them opening is a question asked.
		oldF, oldL, oldQ, oldM := folderDialog, listDialog, questionDialog, messageDialog
		folderDialog = func(...zenity.Option) (string, error) { asked = true; return q, nil }
		listDialog = func(string, []string, ...zenity.Option) (string, error) { asked = true; return "", zenity.ErrCanceled }
		questionDialog = func(string, ...zenity.Option) error { asked = true; return zenity.ErrCanceled }
		messageDialog = func(string, ...zenity.Option) error { asked = true; return nil }
		second, secondErr = makeSet(rows, "darwin")
		folderDialog, listDialog, questionDialog, messageDialog = oldF, oldL, oldQ, oldM
		treeAfter, listAfter = treeOf(t, q), madeLines(t)
	}
	made, err = run(q, first)
	if err != nil || len(made) != 5 {
		t.Fatalf("the first window: %+v %v", made, err)
	}
	for _, m := range made {
		if m.Outcome != MadeFinished {
			t.Errorf("the first window's archive did not finish: %+v", m)
		}
	}
	code, msg := 0, ""
	if secondErr != nil && second == nil {
		code, msg = 1, secondErr.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("Another MailArchive window is making archives now", "nothing was made", "When that window has finished, try again."),
		assure.NoSideEffect(func() bool {
			return !asked && reflect.DeepEqual(treeAfter, treeBefore) && reflect.DeepEqual(listAfter, listBefore)
		}))

	// Once the first window has finished, a later one runs and makes no " (2)".
	if made, err = run(q, nil); err != nil || len(made) != 4 || len(numbered(t, q)) != 0 {
		t.Errorf("a window after the first: %+v %v, numbered %q", made, err, numbered(t, q))
	}
	f, err := job.MadeListFile()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(f), job.MadeSetLockName)); err != nil {
		t.Errorf("the set lock is not beside the made-list: %v", err)
	}
	for _, parent := range []string{p, q} {
		if _, err := os.Lstat(filepath.Join(parent, job.MadeSetLockName)); err == nil {
			t.Errorf("a lock was written in the parent %s", parent)
		}
	}
}
