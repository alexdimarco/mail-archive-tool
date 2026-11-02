package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1026, R36
// delete-archive of a folder whose delete stopped part way begins its plan with "This finishes the
// delete begun DATE." (the 1.5.3 friction review's D1), and the record of the run that finishes it
// names the first run and carries what it removed. Positive twin first: an ordinary plan begins
// with "MailArchive will permanently delete".
func TestTheTerminalPlanSaysItFinishesAPartWayDelete(t *testing.T) {
	e := newCLIDel(t)
	dir := e.plainArchive("Part Way")
	os.Remove(filepath.Join(dir, "notes.txt"))
	a := &scripted{isAttended: true}
	if err := deleteArchive(a, dir, filepath.Join(t.TempDir(), "plan.txt"), "", io.Discard); err != nil || len(a.said) == 0 ||
		!strings.HasPrefix(a.said[0], "MailArchive will permanently delete") {
		t.Fatalf("the positive twin's plan (%v):\n%s", err, strings.Join(a.said, "\n"))
	}

	// A Desktop appears right before step 8: the delete stops part way, its files removed.
	beforeDeleteStep = func(step int) {
		if step == 8 {
			e.desktopRunning("2")
		}
	}
	var out bytes.Buffer
	err := deleteArchive(&scripted{isAttended: true, answers: []string{"Part Way"}}, dir, "", "", &out)
	beforeDeleteStep = func(int) {}
	if exitCode(err) != 3 {
		t.Fatalf("the first run should stop part way: exit %d (%v)\n%s", exitCode(err), err, out.String())
	}
	first := strings.Index(e.logText(), `"run":"`)
	firstRun := e.logText()[first+7 : first+7+16]

	e2 := &cliDel{t: t, home: e.home, lockDir: t.TempDir(), sched: e.sched}
	desktopLockDir = func() (string, error) { return e2.lockDir, nil } // the Desktop shut down
	a = &scripted{isAttended: true, answers: []string{"Part Way"}}
	out.Reset()
	if err := deleteArchive(a, dir, "", "", &out); err != nil {
		t.Fatalf("delete-archive again: %v\n%s", err, out.String())
	}
	if len(a.said) == 0 || !strings.HasPrefix(a.said[0], "This finishes the delete begun ") {
		t.Fatalf("the plan of the run that finishes it:\n%s", strings.Join(a.said, "\n"))
	}
	rec := recordIn(out.String())
	if !strings.Contains(rec, "\nFinishes: the delete begun ") || !strings.Contains(rec, "(run "+firstRun+")") || !strings.Contains(rec, "\nFiles removed earlier: ") {
		t.Fatalf("the record of the run that finishes it:\n%s", rec)
	}
}
