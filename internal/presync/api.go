package presync

import (
	"io"
	"time"
)

// Config is what a caller (the -thunderbird-sync CLI flag in v1, a scheduled job
// later) gives one pre-sync before an archive run. Inputs are the archive's
// discovered sources, used to derive the profile and the watch set when Profile
// is empty; Profile/ProfileNamed carry an explicit -thunderbird-profile (which
// bypasses the profiles.ini membership test, PC3). StateDir holds the
// write-ahead record and the session lock — never the archive directory. Log,
// when non-nil, receives the owner-facing narration (a line when the sync
// begins, since the wait can be minutes, and a line for the result); the manual
// run passes os.Stderr, so the owner is not left watching nothing (§8 step 1).
type Config struct {
	Inputs       []string
	Profile      string
	ProfileNamed bool
	StateDir     string
	Log          io.Writer
}

// Result is one pre-sync's outcome, for the caller to surface as a warning.
type Result struct {
	Outcome Outcome
	// Release, when non-nil, MUST be called by the caller AFTER the archive read: it
	// releases the per-profile lock that the pre-sync holds across the pre-sync and the
	// read, so a second scheduled run cannot pre-sync (and compact) this profile while
	// this run reads it (adversarial finding C2). app.Run defers it.
	Release func()
}

// Default timings (design §4.4.2, §4.5, §4.9.4 "quiet 3 min, up to 20 min"). The
// quiet clock needs a first observed change (the rev-3 L-M5 rule), so the
// opening ~10-minute silence of Thunderbird's first offline poll never ends a
// session early.
const (
	defSettle   = 30 * time.Second
	defQuiet    = 3 * time.Minute
	defMaxWait  = 20 * time.Minute
	defIdleAway = 2 * time.Minute
	// A higher bar to QUIT than to start (adversarial finding C1): starting Thunderbird
	// hidden after a 2-minute idle is unobtrusive, but quitting one must not fire during
	// a normal reading pause, so S8 treats the owner as present until ~10 minutes idle.
	defQuitIdleAway = 10 * time.Minute
	defAwaitPoll    = 2 * time.Second
	defAwaitUp      = 90 * time.Second
	defPollEvery    = 10 * time.Second
	defQuitGrace    = 2 * time.Minute
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
	OutcomeIdentityUnavailable.Code: "The Thunderbird that was started could not be identified for certain, so it was not closed — nothing is ever closed on a guess. The archive ran anyway. Do this: Thunderbird may have been left open — quit it yourself when you are done.",
	OutcomeNotQuiet.Code:            "Thunderbird's mail files were still changing at the maximum wait (20 minutes); it was asked to quit and the archive ran. Some mail may still have been arriving. Do this: the next scheduled run will pick it up, or run `mailarchive -thunderbird-sync` by hand if you need it archived now.",
	OutcomeExitedEarly.Code:         "Thunderbird closed before its mail files went quiet, so the sync may be incomplete. The archive ran anyway. Do this: the next scheduled run will pick up this mail, or run `mailarchive -thunderbird-sync` by hand if you need it archived now.",
	OutcomeQuitNotPermitted.Code:    "Thunderbird was left open because macOS did not allow Mail Archive to close it. Do this: in System Settings > Privacy & Security > Automation, allow Mail Archive to control Thunderbird; because the permission is asked per program, approve it from a run the scheduler starts (`launchctl kickstart gui/<uid>/<label>`), not only from Terminal. The archive ran anyway.",
	OutcomeQuitTimeout.Code:         "Thunderbird did not close within 2 minutes after it was asked to quit, so it was left open. Why: it may be showing a dialog (an unsent message, a password prompt) or still working. Do this: open Thunderbird and quit it, or run again once it is closed. The archive ran anyway.",
	OutcomeQuitSkippedOthers.Code:   "Thunderbird was left open because more than one Thunderbird was running and, on this system, closing it would have closed them all. Do this: quit the one the sync started when you are done. The archive ran anyway.",
	OutcomeOwnerActive.Code:         "Thunderbird was left open because you seemed to be using it when the sync tried to close it. The archive ran anyway.",
	OutcomeSuccessorFound.Code:      "The Thunderbird that was started went away and a different one is now open; it was not touched. The archive ran anyway.",
	OutcomeLeftoverFound.Code:       "A Thunderbird started by an earlier scheduled sync is still open and was not closed. Do this: switch to that Thunderbird window and quit it. The archive ran anyway.",
	OutcomeProfileUnusable.Code:     "No single, safe Thunderbird profile could be determined, so no sync ran. Do this: name the profile with `-thunderbird-profile PATH`. The archive ran anyway.",
	OutcomeAppNotFound.Code:         "Thunderbird was not found in its usual install location, so no sync ran. A Thunderbird installed in a non-standard place cannot be synced in this version. The archive ran anyway.",
	OutcomeRefusedElevated.Code:     "The Thunderbird sync was skipped because Mail Archive is running as root or an administrator — it would create profile files you could not then open. Do this: run it as your normal user. The archive ran anyway.",
	OutcomeStateUnreadable.Code:     "The pre-sync's own state record could not be read, so its recovery information was reset. The archive ran anyway.",
	OutcomeInternalError.Code:       "The Thunderbird pre-sync failed unexpectedly and was recovered; the archive ran anyway.",
}

// SyncNotice is the single source of the §6 owner-facing paragraph (MA-567) and
// carries every disclosure the UX contract's X16 requires: the start-side effects
// (Thunderbird checks all accounts, may send the Outbox, run filters, show
// notifications), the quit-side on-exit actions (e.g. emptying Trash), that an
// already-open or in-use Thunderbird is left alone, and the one-profile limit —
// without ever claiming the sync means a complete download. The schedule preview,
// the install message, the -h text, the GUI card and the README all draw from this
// one constant so they can never drift, and so none of them has to bolt a missing
// disclosure on afterward. Settled by the friction review (2026-10-24).
const SyncNotice = "The Thunderbird sync starts Thunderbird in the background before the backup and waits for its mail to stop arriving, so the archive includes mail that had only reached the server. Because it drives your real Thunderbird, starting it does what opening Thunderbird always does: it checks all your accounts, sends anything waiting in your Outbox, runs your message filters, and may show notifications. When it then asks Thunderbird to quit, your accounts' on-exit actions run too (for example emptying Trash), so mail you deleted since the last backup may be removed before it is archived. A Thunderbird you already have open, or one you are using, is left alone — never started or closed. If you use more than one Thunderbird profile, only the one holding this mailbox is synced."

// MacAutomationNote is the macOS note shown beside the sync wherever it is offered on a
// Mac: the wizard's schedule question and MailArchive Desktop's Settings card both use
// this one constant (design-desktop-settings-card section 7, MA-861), so they cannot drift.
const MacAutomationNote = "On a Mac, the first time a scheduled backup closes Thunderbird, macOS asks once whether Mail Archive may control it. A scheduled backup runs on its own, often overnight, so you would not be there to answer — and until you do, each run leaves Thunderbird open instead of closing it. To answer while you are at the Mac, run one backup yourself first (the Run-now button on the Mail Archive dashboard) and choose Allow when macOS asks, before you rely on the schedule."

// startLine is the narration printed to Config.Log just before the session runs,
// so the owner of a manual run knows the (possibly minutes-long) wait is working
// and not a hang (§8 step 1). Draft wording; the friction review / MA-567 settle
// the final text.
const startLine = "Thunderbird sync: syncing Thunderbird before the archive — this can take several minutes…"

// SummaryLine is the one-line owner-facing result of a session for Config.Log: a
// clean finish, the reason a note session did nothing, or the warning text. It
// returns "" for a cancelled run (the whole run is aborting) so nothing is said.
func SummaryLine(o Outcome) string {
	switch o.Class {
	case ClassWarn:
		if msg, ok := WarnText(o); ok {
			return "Thunderbird sync: " + msg
		}
	case ClassNote:
		if msg, ok := noteText[o.Code]; ok {
			return msg
		}
		return "Thunderbird sync: " + o.Name + "."
	default: // ClassNone: ok or cancelled
		if o == OutcomeOK {
			return "Thunderbird sync: done — Thunderbird was started, its mail settled, and it was closed. Archiving now."
		}
	}
	return ""
}

// noteText holds the owner-facing line for each note outcome (the F-row wording,
// §8); draft, refined by the friction review / MA-567.
var noteText = map[string]string{
	OutcomeNoActivity.Code:          "Thunderbird sync: done — Thunderbird was started and closed; nothing new had arrived.",
	OutcomeAlreadyRunning.Code:      "Thunderbird sync: Thunderbird was already open, so it was not started or closed. Archiving what is on disk.",
	OutcomeOtherProfileRunning.Code: "Thunderbird sync: Thunderbird is open with a different profile, so this profile was not synced. Archiving what is on disk.",
	OutcomeProfileBusy.Code:         "Thunderbird sync: another backup is syncing this Thunderbird profile, so this run skipped it. Archiving what is on disk.",
	OutcomeOwnerActiveSkipped.Code:  "Thunderbird sync: you were using the computer, so Thunderbird was not started. Archiving what is on disk.",
	OutcomeWatchTruncated.Code:      "Thunderbird sync: done (the mail-folder set was very large, so only part of it was watched for quiet). Archiving now.",
}

// narrate writes one owner-facing line to w when both are non-empty.
func narrate(w io.Writer, line string) {
	if w != nil && line != "" {
		_, _ = io.WriteString(w, line+"\n")
	}
}
