package app

import "testing"

// covers: MA-329, R2, R3, R5
// P14: a healthy input is unchanged by fault isolation (a pre-change golden).
func TestHealthyInputsMatchTheGolden(t *testing.T) {
	t.Skip("MA-329: the pre-change golden manifest is not taken yet; lands in FI-S1a (first, before any S1a code)")
}
