package presync

import "testing"

// covers: MA-558
// The S3 crash-recovery table (design section 4.6) in v1 NEVER quits — a leftover
// of ours (an exact identity match) is announced every run while the same
// process stays open, with no expiry; a reboot (boot change) clears the record
// silently; a Thunderbird that is not ours is treated as already-running; an
// unreadable record is warned once and treated as absent.
func TestPresyncRecovery(t *testing.T) {
	const boot = 1_700_000_000
	rec := func(s recordState) record { return record{Version: recordVersion, Boot: boot, State: s, LaunchAt: 100} }

	cases := []struct {
		name       string
		rec        record
		present    bool
		unreadable bool
		o          obs
		want       recoverDecision
	}{
		{"unreadable -> warn once, proceed", record{}, false, true, obs{boot: boot}, recoverDecision{proceed: true, clear: true, outcome: OutcomeStateUnreadable}},
		{"no record -> normal", record{}, false, false, obs{boot: boot}, recoverDecision{proceed: true}},
		{"idle -> normal, clear", rec(stateIdle), true, false, obs{boot: boot}, recoverDecision{proceed: true, clear: true}},
		{"reboot clears silently (syncing+exact)", rec(stateSyncing), true, false, obs{boot: boot + 1, match: matchExact}, recoverDecision{proceed: true, clear: true}},
		{"launching, nothing open -> clear", rec(stateLaunching), true, false, obs{boot: boot, match: matchNoProcess}, recoverDecision{proceed: true, clear: true}},
		{"launching, weak match -> announce, end", rec(stateLaunching), true, false, obs{boot: boot, match: matchWeak}, recoverDecision{outcome: OutcomeLeftoverFound}},
		{"launching, other TB -> owner's, clear", rec(stateLaunching), true, false, obs{boot: boot, match: matchOther}, recoverDecision{proceed: true, clear: true}},
		{"syncing, exact -> leftover, end, never quit", rec(stateSyncing), true, false, obs{boot: boot, match: matchExact}, recoverDecision{outcome: OutcomeLeftoverFound}},
		{"syncing, gone -> clear", rec(stateSyncing), true, false, obs{boot: boot, match: matchNoProcess}, recoverDecision{proceed: true, clear: true}},
		{"syncing, not ours -> already-running note, clear", rec(stateSyncing), true, false, obs{boot: boot, match: matchOther}, recoverDecision{proceed: true, clear: true, outcome: OutcomeAlreadyRunning}},
		{"left-running, exact -> leftover, end", rec(stateLeftRunning), true, false, obs{boot: boot, match: matchExact}, recoverDecision{outcome: OutcomeLeftoverFound}},
		{"left-running, gone -> clear", rec(stateLeftRunning), true, false, obs{boot: boot, match: matchNoProcess}, recoverDecision{proceed: true, clear: true}},
		{"quitting, gone -> clear", rec(stateQuitting), true, false, obs{boot: boot, match: matchNoProcess}, recoverDecision{proceed: true, clear: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := applyRecovery(c.rec, c.present, c.unreadable, c.o); got != c.want {
				t.Errorf("applyRecovery = %+v, want %+v", got, c.want)
			}
		})
	}

	t.Run("a matched leftover is announced with no expiry, however old", func(t *testing.T) {
		old := record{Version: recordVersion, Boot: boot, State: stateLeftRunning, LaunchAt: 1} // ancient launch_at
		got := applyRecovery(old, true, false, obs{boot: boot, match: matchExact})
		if got.outcome != OutcomeLeftoverFound || got.proceed || got.clear {
			t.Errorf("an old matched leftover must still announce and not clear: got %+v", got)
		}
	})
}
