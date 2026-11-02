package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-998, R36, R12
// The check that no MailArchive Desktop runs refuses when a Desktop lock cannot be probed — a
// lock name that is a folder, or a lock this account cannot open (root's after `sudo mailarchive
// desktop`, another account's) — and never writes a lock; a lock folder that cannot be read at a
// re-check mid-delete stops it without saying "nothing was changed" (slice 4's adversarial pass,
// del-s4-A1-02, A2-03).
func TestADesktopLockThatCannotBeProbedRefusesTheDeleteVerbs(t *testing.T) {
	e := newCLIDel(t)
	dir := e.plainArchive("Archive P")
	holder := []byte("pid=999999 started=2026-11-01T10:00:00Z host=elsewhere nonce=0123 verb=desktop\n")
	lock := filepath.Join(e.lockDir, "desktop.lock")
	ro := filepath.Join(e.lockDir, "desktop-s3.lock")
	if err := os.WriteFile(lock, holder, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ro, holder, 0o400); err != nil {
		t.Fatal(err)
	}

	// Positive twin: free locks — one left by a Desktop that ended, one marked read-only — are no
	// Desktop: the plan is shown, and neither lock is written.
	a := &scripted{isAttended: true, answers: []string{"not the name"}}
	_ = deleteArchive(a, dir, "", "", io.Discard)
	assure.Reached(t, prompted(a, "Type the folder's name, Archive P"), "the plan with only free Desktop locks")
	for _, p := range []string{lock, ro} {
		if got, err := os.ReadFile(p); err != nil || !bytes.Equal(got, holder) {
			t.Fatalf("the check wrote the Desktop's lock %s: %q (%v)", p, got, err)
		}
	}
	os.Chmod(ro, 0o600)
	if err := os.Remove(ro); err != nil {
		t.Fatal(err)
	}

	before := tree(t, dir)
	unchanged := func() bool { return sameTree(before, tree(t, dir)) && e.listed(dir) && e.logText() == "" }
	refusedBoth := func(why string) {
		t.Helper()
		for verb, run := range map[string]func(a asker) error{
			"delete-archive": func(a asker) error { return deleteArchive(a, dir, "", "", io.Discard) },
			"unlist-archive": func(a asker) error { return unlistArchive(a, dir, io.Discard) },
		} {
			a := &scripted{isAttended: true, answers: []string{"Archive P", "y"}}
			err := run(a)
			assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
				assure.Names("cannot tell whether MailArchive Desktop is running", "nothing was changed"),
				assure.NoSideEffect(func() bool { return unchanged() && len(a.prompts) == 0 }))
			if t.Failed() {
				t.Fatalf("%s with %s", verb, why)
			}
		}
	}

	// A lock name that is a folder (as a link or a junction would be): cannot tell.
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	refusedBoth("a lock name that is a folder")
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}

	// Off Windows, a lock this account cannot open (root's, another account's): cannot tell.
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		if err := os.WriteFile(lock, holder, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(lock, 0); err != nil {
			t.Fatal(err)
		}
		refusedBoth("a lock this account cannot open")
		os.Chmod(lock, 0o600)
		os.Remove(lock)
	}

	// Mid-delete: the lock folder cannot be read at the re-check before step 4: the stop names
	// the steps done and never says nothing was changed.
	entry := schedule.DefaultNameFor(dir)
	e.sched.installed[entry] = true
	e.sched.onRemove = func(string) {
		desktopLockDir = func() (string, error) { return "", errors.New("the profile folder is not there") }
	}
	var out bytes.Buffer
	err := deleteArchive(&scripted{isAttended: true, answers: []string{"Archive P"}}, dir, "", "", &out)
	e.sched.onRemove = nil
	if exitCode(err) != 3 {
		t.Fatalf("a re-check that cannot tell, before step 4: exit %d (%v), want 3\n%s", exitCode(err), err, out.String())
	}
	res := out.String()
	for _, want := range []string{"cannot tell whether MailArchive Desktop is running", "the profile folder is not there", "The delete stopped before step 4", "Steps done:", "scheduled backup " + entry + " stopped"} {
		if !strings.Contains(res, want) {
			t.Fatalf("the stop does not say %q:\n%s", want, res)
		}
	}
	if strings.Contains(res, "nothing was changed") {
		t.Fatalf("a stop after schedules were stopped says nothing was changed:\n%s", res)
	}
}
