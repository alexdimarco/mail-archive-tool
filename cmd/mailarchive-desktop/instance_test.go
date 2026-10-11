package main

import "testing"

// covers: MA-376, R23, R12, S43
// Skip encoder (catalog-first): the per-account instance, its lock and
// instance file, the handoff, the visible refusals and the port plan.
func TestDesktopInstanceHandoff(t *testing.T) {
	t.Skip("MA-376: the per-account instance lock, instance file and handoff are not built; lands in LC-C2")
}
