package desktop

import "testing"

// covers: MA-1074, R39, S41
// Switch to this archive, and Use this archive, on a locked encrypted archive: the page drawn for it
// is at its Unlock box with the keyboard in it, once, as after Open an archive folder… (MA-1070) —
// the independent review's MED-2 of bc84ca5: on the Windows VM, after a switch, the Unlock box was
// 1,200 to 2,070 pixels down and 32 to 34 Tab presses away, and Switch is the more common way to
// reach a locked archive. Positive twin first: a switch refused asks nothing of the page drawn next
// and draws none. Run under Node (testdata/pagescript/harness.js, switchThenUnlock).
func TestSwitchingToALockedArchiveAsksToUnlockInView(t *testing.T) {
	if passed := runPageScenario(t, "switchThenUnlock", true); len(passed) != 3 {
		t.Errorf("the switchThenUnlock scenario passed %d of its 3 checks: %v", len(passed), passed)
	}
}
