package health

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

func healthyInput(now time.Time) Input {
	return Input{
		Out: "/a", HasManifest: true, Messages: 100, HasIndex: true, Indexed: 100,
		LastRun:       state.LastRun{Status: state.RunOK, Started: now.Add(-2 * time.Hour), Finished: now.Add(-1 * time.Hour), PID: 1},
		LastRunState:  state.LastRunPresent,
		HasDescriptor: true, Desc: schedule.Descriptor{Name: "mailarchive-1234abcd", Interval: "daily", At: "02:00", Exe: "/usr/local/bin/mailarchive", Host: "box"},
		SchedState: schedule.Installed, ExeExists: true, ExeIsThis: true, ThisHost: "box",
		PIDAlive: func(int) bool { return true },
	}
}

// covers: MA-75, R18, S29
// The posture fails closed on missing evidence and every WARN/RED names its
// remedy: a healthy archive is GREEN; missing content, no schedule, an
// unrecorded first run, an unreadable record, a stale run, another host, a
// scheduler that cannot be queried, or a moved binary are WARN; a failed run, a
// run that never finished, or a missing scheduled program are RED. An
// authentication failure gets the secret-rotation remedy.
func TestPosture(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	dead := func(int) bool { return false }

	cases := []struct {
		name   string
		mutate func(in *Input)
		want   string
		needle string
	}{
		{"healthy", func(in *Input) {}, "GREEN", ""},
		{"fillable", func(in *Input) { in.Fillable = 3 }, "WARN", "missing content"},
		{"unknown", func(in *Input) { in.Unknown = 5 }, "WARN", "completeness tracking"},
		{"index behind", func(in *Input) { in.Indexed = 90 }, "WARN", "reindex"},
		{"no schedule", func(in *Input) { in.HasDescriptor = false; in.DescErr = os.ErrNotExist }, "WARN", "no schedule"},
		{"never ran", func(in *Input) { in.LastRunState = state.LastRunAbsent }, "WARN", "no run has ever been recorded"},
		{"unreadable", func(in *Input) { in.LastRunState = state.LastRunUnreadable; in.LastRunErr = errors.New("torn") }, "WARN", "unreadable"},
		{"stale", func(in *Input) {
			in.LastRun.Started = now.Add(-72 * time.Hour)
			in.LastRun.Finished = now.Add(-71 * time.Hour)
		}, "WARN", "twice"},
		{"other host", func(in *Input) { in.Desc.Host = "elsewhere" }, "WARN", "not this one"},
		{"scheduler unavailable", func(in *Input) { in.SchedState = schedule.SchedulerUnavailable }, "WARN", "cannot be queried"},
		{"not installed", func(in *Input) { in.SchedState = schedule.NotInstalled }, "WARN", "-install"},
		{"moved binary", func(in *Input) { in.ExeIsThis = false }, "WARN", "not this binary"},
		{"failed", func(in *Input) { in.LastRun.Status = state.RunFailed; in.LastRun.Error = "boom" }, "RED", "FAILED: boom"},
		{"auth failed", func(in *Input) {
			in.LastRun.Status = state.RunFailed
			in.LastRun.Error = "token: AADSTS7000215 invalid client secret"
		}, "RED", "rotate"},
		{"never finished (lock free, pid reused)", func(in *Input) { in.LastRun.Status = state.RunRunning; in.LockHeld = false }, "RED", "never finished"},
		{"in progress (lock held)", func(in *Input) {
			in.LastRun.Status = state.RunRunning
			in.LastRun.Started = now.Add(-10 * time.Minute)
			in.LockHeld = true
			in.PIDAlive = dead // a pid check must not override the lock
		}, "GREEN", "in progress"},
		{"exe gone", func(in *Input) { in.ExeExists = false }, "RED", "no longer exists"},
		{"cancelled", func(in *Input) { in.LastRun.Status = state.RunCancelled }, "WARN", "cancelled"},
	}
	for _, c := range cases {
		in := healthyInput(now)
		c.mutate(&in)
		rep := Assess(in, now)
		if rep.Posture != c.want {
			t.Errorf("%s: posture %s, want %s (%v)", c.name, rep.Posture, c.want, rep.Reasons)
		}
		if c.needle != "" && !strings.Contains(strings.Join(rep.Reasons, "\n"), c.needle) {
			t.Errorf("%s: reasons lack %q: %v", c.name, c.needle, rep.Reasons)
		}
		if rep.Posture != "GREEN" && len(rep.Reasons) == 0 {
			t.Errorf("%s: %s without a reason", c.name, rep.Posture)
		}
	}
	// The rendered summary carries the posture and every reason.
	in := healthyInput(now)
	in.Fillable = 2
	lines := strings.Join(Summary(in, Assess(in, now)), "\n")
	for _, want := range []string{"Archive:", "Messages:", "Incomplete: 2", "Last run:", "Schedule:", "Posture:    WARN", "missing content"} {
		if !strings.Contains(lines, want) {
			t.Errorf("summary lacks %q:\n%s", want, lines)
		}
	}
}
