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
func Query(name string) State {
	switch runtime.GOOS {
	case "darwin":
		_, err := os.Stat(launchdPlistPath(name))
		return launchdState(err == nil)
	case "windows":
		_, err := exec.Command("schtasks", "/Query", "/TN", name).CombinedOutput()
		return schtasksState(err)
	default:
		out, err := crontabList()
		return cronState(name, out, err)
	}
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
