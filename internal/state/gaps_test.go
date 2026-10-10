package state

import "testing"

// covers: MA-323, R1, R5, S46
// The capture-gap ledger's lifecycle (fault-isolation §4.2, §4.3).
func TestCaptureGapLedgerLifecycle(t *testing.T) {
	t.Skip("MA-323: the capture-gap ledger (state.CaptureGap, ObserveGap, CloseUnobserved, the lifetime caps, Load's sanitisation) is not built yet; lands in FI-S1a")
}
