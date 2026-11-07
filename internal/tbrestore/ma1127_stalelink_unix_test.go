//go:build !windows

package tbrestore

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// covers: MA-1127, R40, S67
// ADV-L1 (settled 2026-11-07, the lead's call): a `.parentlock` LINK — Mozilla's fallback
// lock where fcntl fails, which a crash leaves behind naming a pid that is gone — is
// judged one way at the plan's check and at Start, every time, never "closed" at Check
// and "open" at Start. Positive twin first: a profile with an ordinary .parentlock is
// taken. A link naming a live pid is held: W-RUNNING at both. A link naming a dead pid is
// worded as what it is — a lock left behind — with what to do, at both, and again on a
// second Start; never "Thunderbird is open"; the profile unchanged. And the race (the
// link appears between the check and the open, O_NOFOLLOW's ELOOP) gets the same words.
func TestAStaleParentlockLinkIsWordedOneWayAtCheckAndStart(t *testing.T) {
	noProcesses(t)
	twin, _ := localProfile(t)
	writeFile(t, filepath.Join(twin, ParentLockName), "")
	l, err := TakeProfileLock(twin)
	if err != nil {
		t.Fatalf("positive twin: an ordinary .parentlock is taken: %v", err)
	}
	l.Release()

	dead := strconv.Itoa(deadPID(t))
	for _, c := range []struct {
		what, target, code string
		names              []string
	}{
		{"a link naming a live pid", "127.0.0.1:+" + strconv.Itoa(os.Getpid()), "W-RUNNING", []string{"Thunderbird is open"}},
		{"a link naming a dead pid", "127.0.0.1:+" + dead, "W-LOCKFAIL", []string{"left behind", "Open Thunderbird with this profile and quit it once", "nothing was restored", "Check again", "another computer"}},
	} {
		profile, _ := localProfile(t)
		file := filepath.Join(profile, ParentLockName)
		if err := os.Symlink(c.target, file); err != nil {
			t.Fatal(err)
		}
		before := snapshot(t, profile)
		s := CheckRunning(profile)
		if !s.Running() {
			t.Fatalf("%s: the plan's check finds Thunderbird closed: %+v", c.what, s)
		}
		planErr := RunningRefusal(profile, s)
		_, startErr := TakeProfileLock(profile)
		_, againErr := TakeProfileLock(profile)
		unchanged := func() bool { return snapshot(t, profile) == before }
		names := c.names
		if c.code != "W-RUNNING" {
			names = append([]string{file}, names...)
		}
		for i, err := range []error{planErr, startErr, againErr} {
			refusedAs(t, err, c.code, unchanged, names...)
			if c.code != "W-RUNNING" && strings.Contains(err.Error(), "Thunderbird is open") {
				t.Errorf("%s (%d): says Thunderbird is open: %q", c.what, i, err)
			}
		}
		if planErr.Error() != startErr.Error() || startErr.Error() != againErr.Error() {
			t.Errorf("%s: the plan and Start word it differently:\n%q\n%q\n%q", c.what, planErr, startErr, againErr)
		}
	}

	profile, _ := localProfile(t)
	file := filepath.Join(profile, ParentLockName)
	if err := os.Symlink("127.0.0.1:+"+dead, file); err != nil {
		t.Fatal(err)
	}
	refusedAs(t, lockOpenRefusal(profile, &os.PathError{Op: "open", Path: file, Err: unix.ELOOP}), "W-LOCKFAIL", nil, file, "left behind")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("127.0.0.1:+"+strconv.Itoa(os.Getpid()), file); err != nil {
		t.Fatal(err)
	}
	refusedAs(t, lockOpenRefusal(profile, &os.PathError{Op: "open", Path: file, Err: unix.ELOOP}), "W-RUNNING", nil, "Thunderbird is open")
}
