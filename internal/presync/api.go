package presync

import "time"

// Config is what a caller (the -thunderbird-sync CLI flag in v1, a scheduled job
// later) gives one pre-sync before an archive run. Inputs are the archive's
// discovered sources, used to derive the profile and the watch set when Profile
// is empty; Profile/ProfileNamed carry an explicit -thunderbird-profile (which
// bypasses the profiles.ini membership test, PC3). StateDir holds the
// write-ahead record and the session lock — never the archive directory.
type Config struct {
	Inputs       []string
	Profile      string
	ProfileNamed bool
	StateDir     string
}

// Result is one pre-sync's outcome, for the caller to surface as a warning.
type Result struct {
	Outcome Outcome
}

// Default timings (design §4.4.2, §4.5, §4.9.4 "quiet 3 min, up to 20 min"). The
// quiet clock needs a first observed change (the rev-3 L-M5 rule), so the
// opening ~10-minute silence of Thunderbird's first offline poll never ends a
// session early.
const (
	defSettle    = 30 * time.Second
	defQuiet     = 3 * time.Minute
	defMaxWait   = 20 * time.Minute
	defIdleAway  = 2 * time.Minute
	defAwaitPoll = 2 * time.Second
	defAwaitUp   = 90 * time.Second
	defPollEvery = 10 * time.Second
	defQuitGrace = 2 * time.Minute
)

// WarnText returns the what / why / do-this message for a warn-class outcome
// (design §4.9.4), and whether the outcome is a warning at all: ok and note
// classes return ("", false), so the caller surfaces nothing. The archive has
// always run by the time any of these is produced (R32).
func WarnText(o Outcome) (string, bool) {
	if o.Class != ClassWarn {
		return "", false
	}
	if msg, ok := warnText[o.Code]; ok {
		return msg, true
	}
	return "The Thunderbird sync did not complete (" + o.Name + "). The archive ran anyway.", true
}

// warnText holds the draft owner-facing wording for each WARN outcome (design
// §4.9.4; the single §6 source paragraph and the friction review refine these at
// TP4, MA-567). Each is one line of what / why / do-this; the archive ran anyway.
var warnText = map[string]string{
	OutcomeCannotStart.Code:         "Thunderbird could not be started, so the archive ran without a fresh sync. Do this: run `mailarchive -thunderbird-sync` by hand to check it starts; mail that arrived since the last sync may not be in this archive yet.",
	OutcomeNoDesktopSession.Code:    "Thunderbird could not be started because there is no desktop session (the window server was unreachable). The archive ran anyway. Do this: run the sync while signed in to the Mac.",
	OutcomeDidNotStart.Code:         "Thunderbird did not appear within 90 seconds of being started, so the archive ran without a confirmed sync. Do this: run `mailarchive -thunderbird-sync` by hand to check it starts.",
	OutcomeIdentityUnavailable.Code: "The Thunderbird that was started could not be identified for certain, so it was not closed — nothing is ever closed on a guess. The archive ran anyway.",
	OutcomeNotQuiet.Code:            "Thunderbird's mail files were still changing at the maximum wait; it was asked to quit and the archive ran. Some mail may still have been downloading.",
	OutcomeExitedEarly.Code:         "Thunderbird closed before its mail files went quiet, so the sync may be incomplete. The archive ran anyway.",
	OutcomeQuitNotPermitted.Code:    "Thunderbird was left open because macOS did not allow Mail Archive to close it. Do this: in System Settings > Privacy & Security > Automation, allow Mail Archive to control Thunderbird; because the permission is asked per program, approve it from a run the scheduler starts (`launchctl kickstart gui/<uid>/<label>`), not only from Terminal. The archive ran anyway.",
	OutcomeQuitTimeout.Code:         "Thunderbird did not close within 2 minutes after it was asked to quit, so it was left open. Why: it may be showing a dialog (an unsent message, a password prompt) or still working. Do this: open Thunderbird and quit it, or run again once it is closed. The archive ran anyway.",
	OutcomeOwnerActive.Code:         "Thunderbird was left open because you seemed to be using it when the sync tried to close it. The archive ran anyway.",
	OutcomeSuccessorFound.Code:      "The Thunderbird that was started went away and a different one is now open; it was not touched. The archive ran anyway.",
	OutcomeLeftoverFound.Code:       "A Thunderbird started by an earlier scheduled sync is still open and was not closed. Do this: quit it from the Dock. The archive ran anyway.",
	OutcomeProfileUnusable.Code:     "No single, safe Thunderbird profile could be determined, so no sync ran. Do this: name the profile with `-thunderbird-profile PATH`. The archive ran anyway.",
	OutcomeAppNotFound.Code:         "Thunderbird was not found in /Applications or ~/Applications, so no sync ran. A Thunderbird in a non-standard place cannot be synced in this version. The archive ran anyway.",
	OutcomeRefusedElevated.Code:     "The Thunderbird sync was skipped because Mail Archive is running as root or an administrator — it would create profile files you could not then open. Do this: run it as your normal user. The archive ran anyway.",
	OutcomeStateUnreadable.Code:     "The pre-sync's own state record could not be read, so its recovery information was reset. The archive ran anyway.",
	OutcomeInternalError.Code:       "The Thunderbird pre-sync failed unexpectedly and was recovered; the archive ran anyway.",
}
