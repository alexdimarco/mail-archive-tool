package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

func healthyInput(now time.Time) statusInput {
	return statusInput{
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
func TestStatusPosture(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	dead := func(int) bool { return false }

	cases := []struct {
		name   string
		mutate func(in *statusInput)
		want   string
		needle string
	}{
		{"healthy", func(in *statusInput) {}, "GREEN", ""},
		{"fillable", func(in *statusInput) { in.Fillable = 3 }, "WARN", "missing content"},
		{"unknown", func(in *statusInput) { in.Unknown = 5 }, "WARN", "completeness tracking"},
		{"index behind", func(in *statusInput) { in.Indexed = 90 }, "WARN", "reindex"},
		{"no schedule", func(in *statusInput) { in.HasDescriptor = false; in.DescErr = os.ErrNotExist }, "WARN", "no schedule"},
		{"never ran", func(in *statusInput) { in.LastRunState = state.LastRunAbsent }, "WARN", "no run has ever been recorded"},
		{"unreadable", func(in *statusInput) { in.LastRunState = state.LastRunUnreadable; in.LastRunErr = errors.New("torn") }, "WARN", "unreadable"},
		{"stale", func(in *statusInput) {
			in.LastRun.Started = now.Add(-72 * time.Hour)
			in.LastRun.Finished = now.Add(-71 * time.Hour)
		}, "WARN", "twice"},
		{"other host", func(in *statusInput) { in.Desc.Host = "elsewhere" }, "WARN", "not this one"},
		{"scheduler unavailable", func(in *statusInput) { in.SchedState = schedule.SchedulerUnavailable }, "WARN", "cannot be queried"},
		{"not installed", func(in *statusInput) { in.SchedState = schedule.NotInstalled }, "WARN", "-install"},
		{"moved binary", func(in *statusInput) { in.ExeIsThis = false }, "WARN", "not this binary"},
		{"failed", func(in *statusInput) { in.LastRun.Status = state.RunFailed; in.LastRun.Error = "boom" }, "RED", "FAILED: boom"},
		{"auth failed", func(in *statusInput) {
			in.LastRun.Status = state.RunFailed
			in.LastRun.Error = "token: AADSTS7000215 invalid client secret"
		}, "RED", "rotate"},
		{"never finished", func(in *statusInput) { in.LastRun.Status = state.RunRunning; in.PIDAlive = dead }, "RED", "never finished"},
		{"in progress", func(in *statusInput) {
			in.LastRun.Status = state.RunRunning
			in.LastRun.Started = now.Add(-10 * time.Minute)
		}, "GREEN", "in progress"},
		{"exe gone", func(in *statusInput) { in.ExeExists = false }, "RED", "no longer exists"},
		{"cancelled", func(in *statusInput) { in.LastRun.Status = state.RunCancelled }, "WARN", "cancelled"},
	}
	for _, c := range cases {
		in := healthyInput(now)
		c.mutate(&in)
		rep := assess(in, now)
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
}

// covers: MA-75, R18, R12, S29
// `status` on a real archive reports and exits 0; on a directory with neither
// manifest nor descriptor it refuses naming the directory.
func TestStatusCLI(t *testing.T) {
	out := t.TempDir()
	if code, stderr := runCLI("-input", "../../testdata/support.pst", "-out", out); code != 0 {
		t.Fatalf("export failed (%d): %s", code, stderr)
	}
	stdout, err := exec.Command(testBin, "status", "-out", out).Output()
	if err != nil {
		t.Fatalf("status refused a real archive: %v", err)
	}
	for _, want := range []string{"Archive:", "Messages:", "Last run:", "Schedule:", "Posture:"} {
		if !strings.Contains(string(stdout), want) {
			t.Errorf("status output lacks %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(string(stdout), "ok") {
		t.Errorf("status did not report the successful run:\n%s", stdout)
	}

	empty := t.TempDir()
	code, stderr := runCLI("status", "-out", empty)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names(filepath.Base(empty), "no manifest"))

	code, stderr = runCLI("status")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-out"))
}
