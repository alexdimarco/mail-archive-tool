package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-992, R36, R12
// unlist-archive removes a row of Your archives at the terminal after asking, keeping the folder,
// and stops only the schedules its question named (design-archive-delete §3.3, §3.2, §5.2, §7).
func TestUnlistArchiveRemovesARowAfterAsking(t *testing.T) {
	e := newCLIDel(t)

	// Positive twin: the question names the schedule; yes removes the row and stops it.
	dir := e.plainArchive("Archive I")
	entry := schedule.DefaultNameFor(dir)
	e.sched.installed[entry] = true
	before := tree(t, dir)
	a := &scripted{isAttended: true, answers: []string{"y"}}
	var out bytes.Buffer
	if err := unlistArchive(a, dir, &out); err != nil {
		t.Fatalf("unlist-archive: %v\n%s\n%s", err, a.transcript(), out.String())
	}
	assure.Reached(t, prompted(a, "Its scheduled backup "+entry+" will also be stopped. Remove from list? The folder and its files are kept."), "the question naming the schedule")
	if e.listed(dir) || !sameTree(before, tree(t, dir)) {
		t.Fatal("the row was kept or the folder changed")
	}
	if r := e.sched.removedNames(); len(r) != 1 || r[0] != entry {
		t.Fatalf("the scheduler removed %v, want [%s]", r, entry)
	}
	for _, want := range []string{"Removed from Your archives. The folder and its files are kept.", "Stopped the scheduled backup " + entry} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the result does not say %q:\n%s", want, out.String())
		}
	}

	j := e.plainArchive("Archive J")
	jEntry := schedule.DefaultNameFor(j)
	e.sched.installed[jEntry] = true
	beforeJ := tree(t, j)
	unchanged := func() bool { return e.listed(j) && sameTree(beforeJ, tree(t, j)) && e.sched.installed[jEntry] }

	// Enter alone is no: nothing changed.
	err := unlistArchive(&scripted{isAttended: true, answers: []string{""}}, j, io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("nothing was changed"), assure.NoSideEffect(unchanged))

	// A folder that is not a row is refused.
	notRow := e.t.TempDir()
	err = unlistArchive(&scripted{isAttended: true, answers: []string{"y"}}, notRow, io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("not in Your archives", notRow), assure.NoSideEffect(unchanged))

	// §7: the archive in use, reachable, is refused.
	if err := desktop.UpdateSettings(e.settingsPath(), "test", func(s *desktop.Settings) { s.Out = j }); err != nil {
		t.Fatal(err)
	}
	err = unlistArchive(&scripted{isAttended: true, answers: []string{"y"}}, j, io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("This is the archive in use", "Switch to another archive"), assure.NoSideEffect(unchanged))
	if err := desktop.UpdateSettings(e.settingsPath(), "test", func(s *desktop.Settings) { s.Out = "" }); err != nil {
		t.Fatal(err)
	}

	// A Desktop started while the question is asked: refused before anything is changed.
	var release func()
	h := &hooked{scripted: &scripted{isAttended: true, answers: []string{"y"}}, at: "Remove from list?", hook: func() { release = e.desktopRunning("") }}
	err = unlistArchive(h, j, io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("MailArchive Desktop is running", "Use the Desktop, or shut it down first"), assure.NoSideEffect(unchanged))
	release()

	// Stopped part way: the first schedule stopped, the second fails; the row is kept, exit 3.
	e.sched.installed[jEntry+"-verify"] = true
	e.sched.failOn[jEntry+"-verify"] = errors.New("Access is denied")
	out.Reset()
	err = unlistArchive(&scripted{isAttended: true, answers: []string{"y"}}, j, &out)
	if exitCode(err) != 3 {
		t.Fatalf("Remove from list stopped part way: exit %d (%v), want 3\n%s", exitCode(err), err, out.String())
	}
	if !e.listed(j) || !strings.Contains(out.String(), "Already stopped: "+jEntry) || !strings.Contains(out.String(), "Access is denied") {
		t.Fatalf("the part-way stop:\n%s", out.String())
	}
}
