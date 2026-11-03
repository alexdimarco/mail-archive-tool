package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1057, R36
// delete-archive of a folder this computer began deleting, now gone with its parent reachable and no
// `folder removed` line (§15.8 question 2, the owner's ruling Q4; the terminal's form): it says the
// question's words and asks "Was it finished elsewhere?" (No by default). No, or Enter, changes
// nothing, exit 1. Yes shows the plan, which says so, then asks for the folder's name as any delete
// does; the delete stops this computer's schedule for it, removes its row and says "Finished here.
// The folder was not found here; you said this delete was finished elsewhere.", and its record
// says so. Positive twin first: with the drive unplugged (its parent gone too) it is "Reconnect the
// drive or folder first", and nothing is asked.
func TestMA1057_DeleteArchiveAsksWhetherItWasFinishedElsewhere(t *testing.T) {
	e := newCLIDel(t)
	gone := func(name string) (string, string) {
		t.Helper()
		drive := filepath.Join(t.TempDir(), "Drive")
		dir := filepath.Join(drive, name)
		e.archiveAt(dir)
		e.list(dir)
		ma1052Started(t, e, dir)
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		return dir, drive
	}

	// Positive twin.
	unplugged, drive := gone("Unplugged")
	if err := os.RemoveAll(drive); err != nil {
		t.Fatal(err)
	}
	a := &scripted{isAttended: true, answers: []string{"y", "Unplugged"}}
	err := deleteArchive(a, unplugged, "", "", &bytes.Buffer{})
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("Reconnect the drive or folder first", unplugged),
		assure.NoSideEffect(func() bool { return len(a.prompts) == 0 && e.listed(unplugged) }))

	dir, _ := gone("Finished elsewhere")
	def := schedule.DefaultNameFor(dir)
	e.sched.installed[def] = true
	logBefore := e.logText()
	for _, answer := range []string{"n", ""} {
		a := &scripted{isAttended: true, answers: []string{answer}}
		err := deleteArchive(a, dir, "", "", &bytes.Buffer{})
		assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("nothing was changed"),
			assure.NoSideEffect(func() bool {
				return e.listed(dir) && len(e.sched.removedNames()) == 0 && e.logText() == logBefore
			}))
		if len(a.prompts) != 1 || a.prompts[0] != elsewhereAsk+" [y/N] " || !contains(a.said, archdelete.ElsewhereQuestion(archdelete.Plain)) {
			t.Fatalf("answer %q: prompts %q said %q", answer, a.prompts, a.said)
		}
	}

	a = &scripted{isAttended: true, answers: []string{"y", "Finished elsewhere"}}
	var out bytes.Buffer
	if err := deleteArchive(a, dir, "", "", &out); err != nil {
		t.Fatalf("yes: %v\n%s", err, out.String())
	}
	if !contains(a.said, archdelete.MsgElsewhereSaid) {
		t.Fatalf("the plan says it: %q", a.said)
	}
	if e.listed(dir) || !contains(e.sched.removedNames(), def) || !strings.Contains(out.String(), "Finished here. "+archdelete.MsgElsewhereSaid) ||
		!strings.Contains(out.String(), "\nFolder here: the folder was not found here; you said this delete was finished elsewhere\n") {
		t.Fatalf("listed %v removed %q:\n%s", e.listed(dir), e.sched.removedNames(), out.String())
	}
	if exists(dir) {
		t.Fatal("the folder is not made again")
	}
}
