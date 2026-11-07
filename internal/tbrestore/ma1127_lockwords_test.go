package tbrestore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1127, R40, S67
// The words for a lock file that cannot be made, opened, asked or locked (the MA-1095
// lab's BUG-2, settled 2026-11-07): only a lock actually held — open by another program,
// by this process, a link at the lock name, or a conflicting lock at the take — says
// W-RUNNING, "Thunderbird is open"; a read-only medium is W-LOCKRO, not allowed is
// W-LOCKPERM, anything else W-LOCKFAIL naming the error; and the plan's running check
// words a lock file it could not open by its cause unless a Thunderbird process was also
// seen. Each is a refusal (nothing is taken); the errors are this platform's own.
func TestLockFailuresAreWordedByTheirCause(t *testing.T) {
	profile := filepath.Join("p", "x.default")
	file := filepath.Join(profile, ParentLockName)
	wrap := func(err error) error { return &os.PathError{Op: "open", Path: file, Err: err} }
	for _, c := range []struct {
		what  string
		err   error
		take  bool
		code  string
		names []string
	}{
		{"held here", errHeldHere, false, "W-RUNNING", []string{"Thunderbird is open"}},
		{"held (open)", wrap(heldOpenErr()), false, "W-RUNNING", []string{"Thunderbird is open"}},
		{"held (take)", wrap(heldTakeErr()), true, "W-RUNNING", []string{"Thunderbird is open"}},
		{"not allowed", wrap(permErr()), false, "W-LOCKPERM", []string{"not allowed", file, "read-only network share", "nothing was restored", "Check again"}},
		{"read-only", wrap(readOnlyErrno()), false, "W-LOCKRO", []string{"read-only drive", profile, "nothing was restored", "Check again"}},
		{"read-only (take)", wrap(readOnlyErrno()), true, "W-LOCKRO", []string{"read-only drive"}},
		{"other", wrap(errors.New("the share went away")), false, "W-LOCKFAIL", []string{file, "the share went away", "nothing was restored"}},
		{"other (take)", wrap(otherTakeErr()), true, "W-LOCKFAIL", []string{file, "nothing was restored"}},
	} {
		var err error
		if c.take {
			err = lockTakeRefusal(profile, c.err)
		} else {
			err = lockOpenRefusal(profile, c.err)
		}
		refusedAs(t, err, c.code, nil, c.names...)
		if c.code != "W-RUNNING" && strings.HasPrefix(err.Error(), "Thunderbird is open") {
			t.Errorf("%s: says Thunderbird is open: %q", c.what, err)
		}
		if strings.Count(err.Error(), file) > 1 {
			t.Errorf("%s: names the lock file twice: %q", c.what, err)
		}
	}
	// The plan's check: a lock file it could not open, alone, worded by its cause; with a
	// Thunderbird process, or a hold seen, W-RUNNING.
	refusedAs(t, RunningRefusal(profile, RunningSignal{ByLock: true, LockErr: wrap(permErr())}), "W-LOCKPERM", nil, file)
	refusedAs(t, RunningRefusal(profile, RunningSignal{ByLock: true, ByProcess: true, LockErr: wrap(permErr())}), "W-RUNNING", nil, "Thunderbird is open")
	refusedAs(t, RunningRefusal(profile, RunningSignal{ByLock: true}), "W-RUNNING", nil, "Thunderbird is open")
}

// covers: MA-1127, R40, S67
// On a real profile folder (the lab's read-only share stands in for here by this
// platform's own refusal to make or open the lock file): a healthy profile's lock is taken
// first (positive twin); then a lock file this account may not make or open is refused at
// the plan's check and at Start in words that name the lock file and the cause, never
// "Thunderbird is open", and nothing in the profile is changed. A lock name that is a
// folder (macOS, Linux) is W-LOCKFAIL. Where the test runs with every permission (root),
// only the folder leg can be made on a real disk; the table above covers the rest.
func TestALockFileThatCannotBeMadeIsNotThunderbirdOpen(t *testing.T) {
	noProcesses(t)
	twin, _ := localProfile(t)
	l, err := TakeProfileLock(twin)
	if err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	l.Release()

	for _, leg := range assure.Reached(t, unmakeableLocks(t), "this platform's legs") {
		profile, _ := localProfile(t)
		// The profile as it is with the leg set, read with the leg undone (a file this
		// account may not read cannot be read for the comparison either).
		leg.set(t, profile)()
		before := snapshot(t, profile)
		undo := leg.set(t, profile)
		s := CheckRunning(profile)
		var planErr error
		if s.Running() {
			planErr = RunningRefusal(profile, s)
		}
		_, startErr := TakeProfileLock(profile)
		undo()
		after := snapshot(t, profile)
		for _, err := range []error{planErr, startErr} {
			if err == nil && leg.atStartOnly {
				continue // the plan's probe does not open a lock file that is not there
			}
			refusedAs(t, err, leg.code, func() bool { return after == before }, append([]string{"nothing was restored"}, leg.names(profile)...)...)
			if strings.HasPrefix(err.Error(), "Thunderbird is open") {
				t.Errorf("%s: says Thunderbird is open: %q", leg.what, err)
			}
		}
		if startErr == nil {
			t.Fatalf("%s: Start took a lock it could not make", leg.what)
		}
	}
}

// lockLeg is one way, on this platform, to make a profile whose lock file cannot be made
// or opened: set makes it so and returns its undo.
type lockLeg struct {
	what        string
	code        string
	atStartOnly bool // the plan's check sees nothing (no lock file yet); Start refuses
	set         func(t *testing.T, profile string) (undo func())
	names       func(profile string) []string
}
