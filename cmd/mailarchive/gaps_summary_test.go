package main

import "testing"

// covers: MA-328, R18, R12, S48
// The run summary's "Not captured" block and "Recovered" line (§4.10).
func TestSummaryNotCaptured(t *testing.T) {
	t.Skip("MA-328: printSummary's Not captured block, Recovered line and the .pst in-place-repair remedy are not built yet; lands in FI-S1a")
}
