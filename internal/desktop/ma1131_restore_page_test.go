package desktop

import "testing"

// Restore to Thunderbird's page (design-restore-thunderbird §7.3, §6.2, §7.4; slice R7), run
// under Node (testdata/pagescript/harness.js) on the served dashboard.js. The routes and the
// server's checks are proven in ma1130_restore_routes_test.go; these prove what the page does with
// their replies: each step in view through showStep, the keyboard on its first control, its own
// status line saying what is needed.

// covers: MA-1131, R40, X15
// A plain archive: the profiles (the pre-selected one ticked, the keyboard on it; one that cannot
// be used shown with why and not offered), what to restore and the tree asked for by the list ID
// and position, a refusal said in place, the plan's lines in the engine's order with the keyboard
// on Restore, the run with W-WAIT for the whole of it and Cancel, its progress and the check, the
// result in the engine's words and Save this record saving the record under its name. Positive
// twin first: a page with no restore running opens nothing.
func TestTheRestorePageBringsEachStepIntoView(t *testing.T) {
	if passed := runPageScenario(t, "restoreFlow"); len(passed) != 11 {
		t.Errorf("the restoreFlow scenario passed %d of its 11 checks: %v", len(passed), passed)
	}
}

// covers: MA-1131, MA-1128, R37, R40, S72
// An encrypted archive: one profile, so its step is skipped; the plan's tick and second question
// with its reasons; a Restore the server refuses for either brings the keyboard back to that box
// and starts nothing; with both it runs, its progress the counts the server sends; Save this
// record says beside it that the record names the restored folders.
func TestTheRestorePageAsksForTheTickAndTheSecondYes(t *testing.T) {
	if passed := runPageScenario(t, "restoreEncrypted"); len(passed) != 6 {
		t.Errorf("the restoreEncrypted scenario passed %d of its 6 checks: %v", len(passed), passed)
	}
}

// covers: MA-1131, R40
// A page drawn while a restore runs follows it; Cancel the restore asks the job slot to stop and
// the stopped result is said; Close gives the keyboard back to Restore to Thunderbird…; a plan the
// server no longer holds goes back to Check; a refusal before any step is said beside the button.
func TestTheRestorePageCancelsAndRecovers(t *testing.T) {
	if passed := runPageScenario(t, "restoreCancel"); len(passed) != 6 {
		t.Errorf("the restoreCancel scenario passed %d of its 6 checks: %v", len(passed), passed)
	}
}
