package presync

import (
	"testing"
	"time"
)

// covers: MA-560
// The shared whole-session watchdog bounds a stalled pre-sync: a session that
// never returns (a watch root on an unresponsive mount, where WalkDir cannot
// honour a deadline) is abandoned at sessionBudget and yields a WARN within the
// budget, so the archive is never blocked (R32). Shared by every OS adapter
// (run.go), so the guard is written and reviewed once, not copied per platform.
func TestRunWatchdogBoundsAStalledSession(t *testing.T) {
	orig := sessionBudget
	defer func() { sessionBudget = orig }()
	sessionBudget = 50 * time.Millisecond

	blocked := make(chan struct{})
	start := time.Now()
	res := runWatchdog(func() Result { <-blocked; return Result{Outcome: OutcomeOK} })
	if res.Outcome != OutcomeInternalError {
		t.Errorf("outcome = %q, want internal-error (the watchdog fired)", res.Outcome.Name)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("watchdog did not honour sessionBudget: took %v", elapsed)
	}
	close(blocked) // release the abandoned goroutine
}
