package schedule

import (
	"os/exec"
	"testing"
)

// covers: MA-78, R18, S29
// Install detection is three-state: an entry whose marker is in the crontab is
// installed; an empty or absent crontab is not installed; a missing crontab
// command (or any other failure) is "scheduler unavailable" — never folded into
// "not installed". Task Scheduler: exit 0 installed, non-zero not installed,
// command missing unavailable; launchd: the plist's existence.
func TestInstalledStates(t *testing.T) {
	name := "mailarchive-1234abcd"
	exit1 := &exec.ExitError{}
	cases := []struct {
		out  string
		err  error
		want State
	}{
		{CronMarker(name) + "\n0 2 * * * /x\n", nil, Installed},
		{"# something else\n", nil, NotInstalled},
		{"no crontab for alex\n", exit1, NotInstalled},
		{"", exec.ErrNotFound, SchedulerUnavailable},
		{"", exit1, NotInstalled},
		{"", errFake, SchedulerUnavailable},
	}
	for _, c := range cases {
		if got := cronState(name, c.out, c.err); got != c.want {
			t.Errorf("cronState(%q, %v) = %v, want %v", c.out, c.err, got, c.want)
		}
	}
	if schtasksState(nil) != Installed || schtasksState(exit1) != NotInstalled || schtasksState(exec.ErrNotFound) != SchedulerUnavailable {
		t.Error("schtasksState classification wrong")
	}
	if launchdState(true) != Installed || launchdState(false) != NotInstalled {
		t.Error("launchdState classification wrong")
	}
	if NotInstalled.String() == Installed.String() || SchedulerUnavailable.String() == "" {
		t.Error("State strings must be distinct and non-empty")
	}
}

type fakeErr struct{}

func (fakeErr) Error() string { return "boom" }

var errFake error = fakeErr{}
