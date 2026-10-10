package app

import "testing"

// covers: MA-327, R1, R7, R19, S48
// The report, README, banner and verify line an inheritor reads (§4.10).
func TestGapReportAndInheritorSurfaces(t *testing.T) {
	t.Skip("MA-327: the report's gap rows, the README class vocabulary, the root banner and verify's line are not built yet; lands in FI-S1a")
}
