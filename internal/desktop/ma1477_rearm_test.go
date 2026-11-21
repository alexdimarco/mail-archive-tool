package desktop

import "testing"

// covers: MA-1477, MA-1475, R36, S41
// An armed delete refused because a backup ran between its check and the delete (changed) is
// checked again and deleted without a second click, once (the owner's ruling of 2026-11-20, Q9:
// "Re-check, then delete (Recommended)"; the friction review's F5): the held result says "A backup
// ran after the check, so MailArchive is checking again below. Nothing was deleted; your typed name
// is kept."; the plan is asked for again armed, and its delete is sent armed with the new plan's
// token and the panel's row (the server checks it as any armed delete); a re-armed delete refused as
// changed again asks for the plan once more un-armed, so it cannot loop. Positive twins first: the
// first delete is sent armed; the re-armed panel sends its delete armed.
func TestAnArmedDeleteMadeStaleByABackupIsCheckedAgainOnce(t *testing.T) {
	for scenario, want := range map[string]int{"rearmDeletes": 3, "rearmOnce": 2} {
		t.Run(scenario, func(t *testing.T) {
			if passed := runPageScenario(t, scenario); len(passed) != want {
				t.Errorf("%s passed %d of its %d checks: %v", scenario, len(passed), want, passed)
			}
		})
	}
}
