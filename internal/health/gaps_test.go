package health

import "testing"

// covers: MA-326, R18, S48
// status's capture-gap rules, gap lines and additive JSON keys (§4.10).
func TestStatusCaptureGaps(t *testing.T) {
	t.Skip("MA-326: the capture_gaps_* rules, the gap lines and the additive JSON keys of status are not built yet; lands in FI-S1a")
}
