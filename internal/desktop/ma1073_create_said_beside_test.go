package desktop

import "testing"

// covers: MA-1073, R39, S41
// What Make a new encrypted archive… says before any step needs the person is said beside the
// button (the independent review's MED-1 of bc84ca5: on the Windows VM a folder that is not empty,
// refused once the wizard's windows answered, was said 2,553 pixels below a 706-pixel window, so the
// page again seemed to stop, as in the owner's report of 2026-11-03): the hint while the windows are
// open, a folder refused, an add already in progress and Cancel in the windows, each with the
// keyboard left on the button, nothing moved, the steps card far below left hidden. Positive twin
// first: windows that answer take the line away and the password step takes over (MA-1070). Run
// under Node (testdata/pagescript/harness.js, createBeside).
func TestWhatTheEncryptedCreateButtonSaysIsSaidBesideIt(t *testing.T) {
	if passed := runPageScenario(t, "createBeside", true); len(passed) != 4 {
		t.Errorf("the createBeside scenario passed %d of its 4 checks: %v", len(passed), passed)
	}
}
