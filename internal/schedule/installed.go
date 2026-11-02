package schedule

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// State is what the host scheduler says about a named entry.
type State int

const (
	NotInstalled State = iota
	Installed
	SchedulerUnavailable // the scheduler itself is missing or cannot be queried
)

func (s State) String() string {
	switch s {
	case Installed:
		return "installed"
	case SchedulerUnavailable:
		return "scheduler unavailable"
	default:
		return "not installed"
	}
}

// Query asks the host scheduler whether the named entry exists. It is a thin
// shell around the pure predicates below, which are what the tests exercise;
// the real-host behaviour is a lab row.
func Query(name string) State { return queryOn(name, runtime.GOOS) }

func queryOn(name, goos string) State {
	switch goos {
	case "darwin":
		_, err := os.Stat(launchdPlistPath(name))
		return launchdState(err == nil)
	case "windows":
		_, err := schtasksRun(SchtasksQueryArgv(name)...)
		return schtasksState(err)
	default:
		out, err := crontabList()
		return cronState(name, out, err)
	}
}

// QueryToStop is Query as a removal asks it — a delete's step 2 and Remove from list
// (archdelete.HostScheduler; the 1.5.3 friction review's H1): on a computer whose scheduler is
// cron, a crontab program that is not installed means no cron entry exists, so the entry is not
// installed and there is nothing to stop. Query keeps saying "scheduler unavailable" there
// (MA-78: status warns that a recorded schedule cannot run on this computer).
func QueryToStop(name string) State { return QueryToStopOn(name, runtime.GOOS) }

// QueryToStopOn is QueryToStop on the scheduler goos selects ("" is this computer's): a test
// passes "linux" to ask cron on any computer.
func QueryToStopOn(name, goos string) State {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "darwin" || goos == "windows" {
		return queryOn(name, goos)
	}
	out, err := crontabList()
	if errors.Is(err, exec.ErrNotFound) {
		return NotInstalled
	}
	return cronState(name, out, err)
}

// cronState classifies `crontab -l`: the command missing → the scheduler is
// unavailable (a container, a systemd-timer-only host); a non-zero exit whose
// output says there is no crontab → not installed; otherwise the managed
// marker decides.
func cronState(name, output string, runErr error) State {
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) && strings.Contains(strings.ToLower(output), "no crontab") {
			return NotInstalled
		}
		if errors.Is(runErr, exec.ErrNotFound) {
			return SchedulerUnavailable
		}
		if errors.As(runErr, &ee) {
			return NotInstalled
		}
		return SchedulerUnavailable
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == CronMarker(name) {
			return Installed
		}
	}
	return NotInstalled
}

func launchdState(plistExists bool) State {
	if plistExists {
		return Installed
	}
	return NotInstalled
}

// schtasksState classifies `schtasks /Query /TN name`: exit 0 → installed; a
// non-zero exit → not installed; the command missing → unavailable.
func schtasksState(runErr error) State {
	if runErr == nil {
		return Installed
	}
	if errors.Is(runErr, exec.ErrNotFound) {
		return SchedulerUnavailable
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		return NotInstalled
	}
	return SchedulerUnavailable
}
