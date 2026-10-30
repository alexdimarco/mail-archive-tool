// Package presync starts Thunderbird out of sight before an archive run, waits
// for its mail to stop arriving, asks it to quit, then lets the archive
// read. It is opt-in and per-schedule, and it never gates the archive (design
// docs/design-thunderbird-presync.md; invariants R30 opt-in/inert, R31 only
// quits what we started, R32 never gates the archive).
//
// This file is the core's vocabulary: the outcomes one session can end in
// (design section 4.9.3) and the severity each contributes to the run. No OS
// calls live here; the state machine (against injected seams) produces these.
package presync

// Class is the severity an outcome contributes to the run record (section 4.9.3).
// WARN is surfaced and counted but never fails the archive and is never RED.
type Class int

const (
	// ClassNone is a clean end (ok) or the run's own cancellation: no code.
	ClassNone Class = iota
	// ClassNote is informational: never a failure, never RED.
	ClassNote
	// ClassWarn is a warning: surfaced and counted, but the archive still runs.
	ClassWarn
)

func (c Class) String() string {
	switch c {
	case ClassNote:
		return "note"
	case ClassWarn:
		return "warn"
	default:
		return "none"
	}
}

// Outcome is how one pre-sync session ended (section 4.9.3). Code is the stable
// identifier surfaced in the run record, status and the GUI health view; it is
// empty for ok and cancelled. The human what/why/remedy text per code is added
// by the surfaces slice (TP2, MA-561), not here.
type Outcome struct {
	Name  string
	Code  string
	Class Class
}

// The outcomes, one per section-4.9.3 row. Package vars so each has a name a
// caller refers to; allOutcomes lists them for the completeness check.
var (
	OutcomeOK                  = Outcome{"ok", "", ClassNone}
	OutcomeNoActivity          = Outcome{"ok-noactivity", "presync_no_activity", ClassNote}
	OutcomeAlreadyRunning      = Outcome{"already-running", "presync_already_running", ClassNote}
	OutcomeOtherProfileRunning = Outcome{"other-profile-running", "presync_other_profile_running", ClassNote}
	OutcomeProfileBusy         = Outcome{"profile-busy", "presync_profile_busy", ClassNote}
	OutcomeCannotStart         = Outcome{"cannot-start", "presync_cannot_start", ClassWarn}
	OutcomeNoDesktopSession    = Outcome{"no-desktop-session", "presync_no_desktop_session", ClassWarn}
	OutcomeDidNotStart         = Outcome{"did-not-start", "presync_did_not_start", ClassWarn}
	OutcomeIdentityUnavailable = Outcome{"identity-unavailable", "presync_identity_unavailable", ClassWarn}
	OutcomeNotQuiet            = Outcome{"not-quiet", "presync_not_quiet", ClassWarn}
	OutcomeExitedEarly         = Outcome{"exited-early", "presync_exited_early", ClassWarn}
	OutcomeQuitNotPermitted    = Outcome{"quit-not-permitted", "presync_quit_not_permitted", ClassWarn}
	OutcomeQuitTimeout         = Outcome{"quit-timeout", "presync_quit_timeout", ClassWarn}
	OutcomeQuitSkippedOthers   = Outcome{"quit-skipped-others", "presync_quit_skipped_others", ClassWarn}
	// OutcomeQuitNoWindow: the quit posted nothing (errQuitNotPosted, C-60), so
	// Thunderbird was left open at once, never said to have been asked to close.
	OutcomeQuitNoWindow = Outcome{"quit-no-window", "presync_quit_no_window", ClassWarn}
	// OutcomeStateUnsaved: a side warning (A9) — the quitting or left-running
	// record could not be saved, so a Thunderbird left open may go unannounced.
	OutcomeStateUnsaved       = Outcome{"state-unsaved", "presync_state_unsaved", ClassWarn}
	OutcomeOwnerActiveSkipped = Outcome{"owner-active-skipped", "presync_owner_active_skipped", ClassNote}
	OutcomeOwnerActive        = Outcome{"owner-active", "presync_owner_active", ClassWarn}
	OutcomeSuccessorFound     = Outcome{"successor-found", "presync_successor_found", ClassWarn}
	OutcomeLeftoverFound      = Outcome{"leftover-found", "presync_leftover_found", ClassWarn}
	OutcomeProfileUnusable    = Outcome{"profile-unusable", "presync_profile_unusable", ClassWarn}
	OutcomeAppNotFound        = Outcome{"app-not-found", "presync_app_not_found", ClassWarn}
	OutcomeRefusedElevated    = Outcome{"refused-elevated", "presync_refused_elevated", ClassWarn}
	OutcomeStateUnreadable    = Outcome{"state-unreadable", "presync_state_unreadable", ClassWarn}
	OutcomeWatchTruncated     = Outcome{"watch-truncated", "presync_watch_truncated", ClassNote}
	OutcomeInternalError      = Outcome{"internal-error", "presync_internal_error", ClassWarn}
	OutcomeCancelled          = Outcome{"cancelled", "", ClassNone}
)

// allOutcomes is every outcome section 4.9.3 defines, in table order. The
// completeness test (landing with MA-560) asserts the set and the class of each.
var allOutcomes = []Outcome{
	OutcomeOK, OutcomeNoActivity, OutcomeAlreadyRunning, OutcomeOtherProfileRunning,
	OutcomeProfileBusy, OutcomeCannotStart, OutcomeNoDesktopSession, OutcomeDidNotStart,
	OutcomeIdentityUnavailable, OutcomeNotQuiet, OutcomeExitedEarly, OutcomeQuitNotPermitted,
	OutcomeQuitTimeout, OutcomeQuitSkippedOthers, OutcomeQuitNoWindow, OutcomeStateUnsaved,
	OutcomeOwnerActiveSkipped, OutcomeOwnerActive, OutcomeSuccessorFound,
	OutcomeLeftoverFound, OutcomeProfileUnusable, OutcomeAppNotFound, OutcomeRefusedElevated,
	OutcomeStateUnreadable, OutcomeWatchTruncated, OutcomeInternalError, OutcomeCancelled,
}
