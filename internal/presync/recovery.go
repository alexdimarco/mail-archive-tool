package presync

// matchKind describes how a current Thunderbird-named process (if any) matches
// the child identity a state record names (design sections 4.6, 4.4.3).
type matchKind int

const (
	matchNoProcess matchKind = iota // no Thunderbird-named process is present
	matchExact                      // matches child_pid, child_start (±1s), the app path and the -profile args
	matchWeak                       // a launching record (no child_pid): a process started at/after launch_at-2s whose path is app
	matchOther                      // a Thunderbird is present but does not match the record
)

// obs is what S3 observes when it reads a record: the current boot time and how
// a current Thunderbird process matches the record's child identity. The Env
// seam supplies it; applyRecovery is a pure decision over it.
type obs struct {
	boot  int64
	match matchKind
}

// recoverDecision is the S3 recovery outcome (design section 4.6). In v1 it
// NEVER quits: a leftover of ours is announced, not closed. proceed continues
// the session to S4 as if there were no prior record; proceed=false ends it here
// (a matched leftover). clear removes the record file. outcome is the note or
// warning to record (the zero Outcome announces nothing).
type recoverDecision struct {
	proceed bool
	clear   bool
	outcome Outcome
}

// applyRecovery is the S3 recovery table (design section 4.6), kept deliberately
// apart from any "may we quit?" question — in v1 the answer is always no. It
// decides only whether to clear the record, whether to proceed, and what to
// announce. A matched leftover is announced every run while the same process
// stays open, with no expiry: the decision looks at the record's identity and
// boot, never at its age.
func applyRecovery(rec record, present, unreadable bool, o obs) recoverDecision {
	if unreadable {
		// Unparseable or a newer version: treat as absent, warn once, continue.
		return recoverDecision{proceed: true, clear: true, outcome: OutcomeStateUnreadable}
	}
	if !present {
		return recoverDecision{proceed: true} // no prior session
	}
	if rec.State == stateIdle {
		return recoverDecision{proceed: true, clear: true} // a spent terminal marker
	}
	if rec.Boot != o.boot {
		// The computer rebooted since the record was written, so nothing it names
		// can be running; clear silently and proceed (a reused pid is a coincidence).
		return recoverDecision{proceed: true, clear: true}
	}
	switch rec.State {
	case stateLaunching:
		switch o.match {
		case matchExact, matchWeak:
			// A process plausibly the one we launched is open: announce a leftover,
			// never quit; the session ends.
			return recoverDecision{outcome: OutcomeLeftoverFound}
		default: // matchNoProcess, matchOther
			// Nothing open, or a Thunderbird that is the owner's: clear and proceed.
			return recoverDecision{proceed: true, clear: true}
		}
	case stateSyncing, stateQuitting, stateLeftRunning:
		switch o.match {
		case matchExact:
			// A leftover of ours, still open: never quit in v1; announce every run
			// while it stays open (no expiry); the session ends.
			return recoverDecision{outcome: OutcomeLeftoverFound}
		case matchOther:
			// A Thunderbird is open but is not ours (pid reused, start/path/args
			// differ): treat as already-running, a note; clear and proceed.
			return recoverDecision{proceed: true, clear: true, outcome: OutcomeAlreadyRunning}
		default: // matchNoProcess (matchWeak does not arise for these states)
			// The owner or the system closed it: clear and proceed.
			return recoverDecision{proceed: true, clear: true}
		}
	}
	// Any unrecognised state: fail safe — proceed and clear, announce nothing.
	return recoverDecision{proceed: true, clear: true}
}
