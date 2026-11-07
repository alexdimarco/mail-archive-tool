//go:build !windows

package tbrestore

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// covers: MA-1127, R40, S67
// PCT-2 (settled 2026-11-07, the lead's call): a Thunderbird lock link whose address names
// ANOTHER computer is held, on macOS and Linux, whatever its pid: Mozilla's
// IsSymlinkStaleLock (toolkit/profile/nsProfileLock.cpp) says "Remote lock: give up even if
// stuck" for an address that is not its own, so a pid there means nothing here. Both names:
// `.parentlock` (Mozilla's fallback on macOS) and `lock` (its fallback name on Linux when
// fcntl locking fails, here beside an ordinary .parentlock no one holds). Refused at the
// plan's check and at Start in the same words, which say plainly that Thunderbird may be
// open on another computer that uses this profile and to close it there, name the lock
// file, and never say that opening and quitting Thunderbird once clears it, or that the
// lock was left behind; the profile unchanged. A pid alive here, a "+" pid, and an address
// that is not a number are each still another computer's. The race (a remote .parentlock
// link appearing between the check and the open, O_NOFOLLOW's ELOOP) gets the same words.
// Positive twins first: a loopback link and a link naming one of this computer's own
// addresses, each with a dead pid, are judged by the pid here, as before (stale).
func TestALockLinkFromAnotherComputerIsHeld(t *testing.T) {
	noProcesses(t)
	dead := strconv.Itoa(deadPID(t))

	locals := []string{"127.0.0.1", "127.0.1.1"}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
				locals = append(locals, n.IP.String())
				break
			}
		}
	}
	for _, addr := range locals {
		p, _ := localProfile(t)
		writeFile(t, filepath.Join(p, ParentLockName), "")
		if err := os.Symlink(addr+":"+dead, filepath.Join(p, "lock")); err != nil {
			t.Fatal(err)
		}
		if s := CheckRunning(p); s.Running() || s.RemoteLink {
			t.Fatalf("positive twin: a `lock` link from this computer (%s) naming a dead pid is stale: %+v", addr, s)
		}
		l, err := TakeProfileLock(p)
		if err != nil {
			t.Fatalf("positive twin: %s: the profile is taken: %v", addr, err)
		}
		l.Release()
	}

	for _, c := range []struct{ what, name, target string }{
		{".parentlock, dead pid", ParentLockName, "192.0.2.1:" + dead},
		{".parentlock, \"+\" pid", ParentLockName, "192.0.2.1:+" + dead},
		{".parentlock, a pid alive here", ParentLockName, "192.0.2.1:+" + strconv.Itoa(os.Getpid())},
		{"lock, dead pid", "lock", "192.0.2.1:" + dead},
		{"lock, \"+\" pid", "lock", "192.0.2.1:+" + dead},
		{"lock, a pid alive here", "lock", "192.0.2.1:" + strconv.Itoa(os.Getpid())},
		{"lock, a name not a number", "lock", "otherbox.example:" + dead},
	} {
		profile, _ := localProfile(t)
		if c.name == "lock" {
			writeFile(t, filepath.Join(profile, ParentLockName), "")
		}
		file := filepath.Join(profile, c.name)
		if err := os.Symlink(c.target, file); err != nil {
			t.Fatal(err)
		}
		before := snapshot(t, profile)
		s := CheckRunning(profile)
		if !s.Running() || !s.RemoteLink || s.StaleLink {
			t.Fatalf("%s: the plan's check: %+v", c.what, s)
		}
		planErr := RunningRefusal(profile, s)
		l, startErr := TakeProfileLock(profile)
		if l != nil {
			l.Release()
		}
		unchanged := func() bool { return snapshot(t, profile) == before }
		for i, err := range []error{planErr, startErr} {
			refusedAs(t, err, "W-RUNNING", unchanged, file, "another computer that uses this profile", "close it there", "nothing was restored", "Check again")
			for _, never := range []string{"quit it once", "left behind", "Thunderbird is open,"} {
				if strings.Contains(err.Error(), never) {
					t.Errorf("%s (%d): says %q: %q", c.what, i, never, err)
				}
			}
		}
		if planErr.Error() != startErr.Error() {
			t.Errorf("%s: the plan and Start word it differently:\n%q\n%q", c.what, planErr, startErr)
		}
	}

	profile, _ := localProfile(t)
	file := filepath.Join(profile, ParentLockName)
	if err := os.Symlink("192.0.2.1:"+dead, file); err != nil {
		t.Fatal(err)
	}
	err := lockOpenRefusal(profile, &os.PathError{Op: "open", Path: file, Err: unix.ELOOP})
	refusedAs(t, err, "W-RUNNING", nil, file, "another computer that uses this profile", "close it there")
	if strings.Contains(err.Error(), "quit it once") {
		t.Errorf("the race: %q", err)
	}
	t.Log(fmt.Sprint(err))
}
