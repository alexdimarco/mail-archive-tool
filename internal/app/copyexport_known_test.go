package app

import (
	"testing"
	"time"
)

// covers: MA-448, R29, R5
// Known item K1 of the extraction workspace's adversarial pass: every identity-confirmed
// access to an export folder (reconcile, Finish as partial, Check and finish, Check this
// export, Open folder, Locate) first probes the folder under a 5 s bound, so a stat that
// hangs on a dead share costs the caller 5 s and never more. The tests that exercise the
// bound shorten it (SetProbeHook with a timeout), so the production value itself was never
// seen: a constant changed to 30 s, or to 50 ms (which would call every slow share missing),
// passed everything. This one leaves the default alone, blocks the probe, and measures.
func TestCopyExportFolderProbeBoundIsFiveSeconds(t *testing.T) {
	if testing.Short() {
		t.Skip("takes 5 s: the production bound itself")
	}
	if copyProbeTimeout != 5*time.Second {
		t.Fatalf("the folder probe's bound is %v, want 5 s", copyProbeTimeout)
	}
	restore := SetProbeHook(func(string) { time.Sleep(8 * time.Second) }, 0) // 0: the default bound stays
	defer restore()
	began := time.Now()
	_, _, timedOut := probeBounded(t.TempDir())
	elapsed := time.Since(began)
	if !timedOut {
		t.Fatalf("a probe that hangs for 8 s was waited for (%v)", elapsed)
	}
	if elapsed < 4500*time.Millisecond || elapsed > 6500*time.Millisecond {
		t.Errorf("a hung probe was given up on after %v, want about 5 s", elapsed)
	}
}
