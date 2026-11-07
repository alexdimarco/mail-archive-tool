package desktop

import "testing"

// covers: MA-1148, R18, S29
// The page script: when /api/activity says a backup was skipped because another one is
// running ("busy"), Archive now, Run now and a new archive's first backup each say the skip's
// words, never a failure or a finish. Positive twin first: a failed Archive now is still said
// as failed. Run under Node (testdata/pagescript/harness.js, busyNotFailed).
func TestTheSkipIsSaidAsASkipOnThePage(t *testing.T) {
	if passed := runPageScenario(t, "busyNotFailed", true); len(passed) != 4 {
		t.Errorf("the busyNotFailed scenario passed %d of its 4 checks: %v", len(passed), passed)
	}
}
