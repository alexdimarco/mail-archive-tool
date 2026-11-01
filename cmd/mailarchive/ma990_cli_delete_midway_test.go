package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-990, R36, R12
// A MailArchive Desktop found by delete-archive's re-check right before steps 4, 8 or 9 stops
// the delete as not fully deleted (exit 3), naming the steps done, never "refused"; the row is
// then "Delete not finished", and delete-archive run again with no Desktop finishes it
// (design-archive-delete §3.3, §5.3, §5.4).
func TestADesktopStartedMidDeleteStopsItPartWay(t *testing.T) {
	e := newCLIDel(t)
	logPath, err := archdelete.DefaultLogPath()
	if err != nil {
		t.Fatal(err)
	}

	// Positive twin: with no Desktop at any re-check the delete completes.
	ok := e.plainArchive("Archive E0")
	if err := deleteArchive(&scripted{isAttended: true, answers: []string{"Archive E0"}}, ok, "", "", io.Discard); err != nil {
		t.Fatalf("a delete with no Desktop: %v", err)
	}

	// A Desktop started while the schedules are stopped (step 2): stopped before step 4.
	dir := e.plainArchive("Archive E")
	entry := schedule.DefaultNameFor(dir)
	e.sched.installed[entry] = true
	var release func()
	e.sched.onRemove = func(string) { release = e.desktopRunning("") }
	var out bytes.Buffer
	err = deleteArchive(&scripted{isAttended: true, answers: []string{"Archive E"}}, dir, "", "", &out)
	e.sched.onRemove = nil
	if exitCode(err) != 3 {
		t.Fatalf("a Desktop found before step 4: exit %d (%v), want 3\n%s", exitCode(err), err, out.String())
	}
	res := out.String()
	for _, want := range []string{"MailArchive Desktop started while the delete was running", "The delete stopped before step 4", "Steps done:", "scheduled backup " + entry + " stopped", "Status: not fully deleted"} {
		if !strings.Contains(res, want) {
			t.Fatalf("the stop does not say %q:\n%s", want, res)
		}
	}
	if strings.Contains(strings.ToLower(res), "refused") {
		t.Fatalf("a delete stopped part way is called refused:\n%s", res)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mailarchive-manifest.json")); err != nil {
		t.Fatalf("a delete stopped before step 4 removed the manifest: %v", err)
	}
	if u, err := archdelete.UnfinishedDelete(logPath, dir, runtime.GOOS); err != nil || u == nil {
		t.Fatalf("the row is not marked Delete not finished: %v %v", u, err)
	}
	release()
	// Run again with no Desktop: it finishes.
	if err := deleteArchive(&scripted{isAttended: true, answers: []string{"Archive E"}}, dir, "", "", io.Discard); err != nil {
		t.Fatalf("delete-archive again: %v", err)
	}
	if e.listed(dir) {
		t.Fatal("delete-archive again left the row")
	}

	// A Desktop started after the files are removed: stopped before step 8, the folder gone.
	f := e.plainArchive("Archive F")
	os.Remove(filepath.Join(f, "notes.txt"))
	beforeDeleteStep = func(step int) {
		if step == 8 {
			release = e.desktopRunning("2")
		}
	}
	out.Reset()
	err = deleteArchive(&scripted{isAttended: true, answers: []string{"Archive F"}}, f, "", "", &out)
	beforeDeleteStep = func(int) {}
	if exitCode(err) != 3 || !strings.Contains(out.String(), "The delete stopped before step 8") {
		t.Fatalf("a Desktop found before step 8: exit %d (%v)\n%s", exitCode(err), err, out.String())
	}
	if _, err := os.Lstat(f); !os.IsNotExist(err) {
		t.Fatalf("the folder is still there: %v", err)
	}
	if !e.listed(f) {
		t.Fatal("a delete stopped before step 9 removed the row")
	}
	release()
	out.Reset()
	if err := deleteArchive(&scripted{isAttended: true, answers: []string{"Archive F"}}, f, "", "", &out); err != nil {
		t.Fatalf("delete-archive again after the folder went: %v\n%s", err, out.String())
	}
	if e.listed(f) || !strings.Contains(out.String(), "The folder is gone") {
		t.Fatalf("delete-archive again did not finish:\n%s", out.String())
	}

	// A Desktop started while the name is being typed: refused at once, nothing changed.
	g := e.plainArchive("Archive G")
	e.sched.installed[schedule.DefaultNameFor(g)] = true
	beforeG, logBefore := tree(t, g), e.logText()
	h := &hooked{scripted: &scripted{isAttended: true, answers: []string{"Archive G"}}, at: "Type the folder's name", hook: func() { release = e.desktopRunning("") }}
	err = deleteArchive(h, g, "", "", io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("MailArchive Desktop is running", "Use the Desktop, or shut it down first"),
		assure.NoSideEffect(func() bool {
			return sameTree(beforeG, tree(t, g)) && e.listed(g) && e.logText() == logBefore && e.sched.installed[schedule.DefaultNameFor(g)]
		}))
	release()
}
