package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1448, R18
// A backup whose "running" record is still there with no process behind it (run_never_finished)
// says, in plain words, what can leave it so (the 1.7.2 friction review's F3): MailArchive
// stopped, the computer shut down or restarted, or this archive's drive disconnected or swapped
// during the backup (1.7.2 then writes nothing into the folder it began in, which went with that
// drive), and what to do. It no longer blames a crash, a kill or power loss alone, nor names a pid.
// Positive twin first: the same record with its run still holding the lock is a run in progress,
// not RED.
func TestABackupThatDidNotFinishSaysWhatCanCauseIt(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	in.LastRun.Status = state.RunRunning
	in.LastRun.Started = now.Add(-10 * time.Minute)
	in.LockHeld = true
	if rep := Assess(in, now); rep.Posture != "GREEN" || !strings.Contains(strings.Join(rep.Reasons, "\n"), "in progress") {
		t.Fatalf("positive twin: a run still holding the lock gave %s %v", rep.Posture, rep.Reasons)
	}
	in.LockHeld = false
	in.PIDAlive = func(int) bool { return false }
	rep := Assess(in, now)
	want := "the backup started " + in.LastRun.Started.Local().Format("2006-01-02 15:04 MST") + " did not finish and could not record how it ended: MailArchive was stopped, the computer shut down or restarted, or this archive's drive was disconnected or swapped during the backup. Run the backup again."
	reasons := strings.Join(rep.Reasons, "\n")
	if rep.Posture != "RED" || !strings.Contains(reasons, want) {
		t.Fatalf("posture %s, reasons %q; want RED with %q", rep.Posture, reasons, want)
	}
	for _, gone := range []string{"crash, kill, or power loss", "pid"} {
		if strings.Contains(reasons, gone) {
			t.Errorf("the reasons still say %q: %q", gone, reasons)
		}
	}
	if !hasCode(rep, "run_never_finished") {
		t.Errorf("the reason code is not run_never_finished: %v", rep.Codes)
	}
}
