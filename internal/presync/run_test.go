package presync

import "testing"

// covers: MA-560
// The whole-session watchdog never lets the pre-sync gate the archive (R32): a
// panic inside the session (resolve, launch, quit, an unsafe syscall read) is
// recovered to presync_internal_error rather than crashing the process — it
// happens in the watchdog's own goroutine, where the caller's recover (app.Run's
// hook guard) cannot reach it (adversarial finding F4). A normal session returns
// its own outcome unchanged.
func TestRunWatchdogRecoversPanic(t *testing.T) {
	if res := runWatchdog(func() Result { panic("boom") }); res.Outcome != OutcomeInternalError {
		t.Errorf("a panicking session = %v, want presync_internal_error (R32)", res.Outcome)
	}
	if res := runWatchdog(func() Result { return Result{Outcome: OutcomeOK} }); res.Outcome != OutcomeOK {
		t.Errorf("a normal session = %v, want OK", res.Outcome)
	}
}
