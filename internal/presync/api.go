package presync

import (
	"context"
	"fmt"
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
	// Stop ends the session AT ONCE with no quit request: the session context is
	// derived from it, and the watchdog returns when it ends, abandoning the session
	// (design-presync-attended §2.6, C-3, C-5). Separate from the run's ctx, whose
	// cause ErrCancelAndQuit (the Cancel button) is handled by A4(ii). nil means
	// never. No caller sets it before the Desktop's Lock and Shut down use it.
	Stop context.Context

	// Attended: the person started this run and is watching it, at this computer
	// (design-presync-attended §2.2, R38). The S4 and S8 owner-activity checks are
	// skipped; every other check stays (§2.3), and a profile the job names must also
	// be listed in profiles.ini (C-18). Never persisted: no job file, descriptor,
	// schedule or record carries it (C-36), so it has no JSON tag anywhere.
	Attended bool
	// Surface picks the words of the end line: SurfaceDesktop names buttons,
	// SurfaceCLI names none (§4.6, C-38).
	Surface string
	// GOOS picks the platform words of the end line (the Dock, or the taskbar);
	// "" means runtime.GOOS (C-24).
	GOOS string
	// Phase, when set, is told "starting", "waiting" and "cancelling" as the session
	// moves (§4.6, C-19). The session makes no Phase call once the watchdog has
	// abandoned it (A15, C-71).
	Phase func(phase string, since time.Time)
	// KeepOpen is the "Keep Thunderbird open" press (C-46, OQ-1). It is read only
	// where a quit can be sent, so it has no reader before slice AS-Q (C-50).
	KeepOpen func() bool
	// NeverQuit: an attended run on this platform never asks Thunderbird to close,
	// at quiet or on Cancel (§2.8, OQ-1). presyncjob sets it from
	// AttendedQuitAllowed(goos). Ignored when !Attended.
	NeverQuit bool
	// EndWait, when it ends during S7, ends the wait for mail at once: the person
	// pressed "Stop waiting and back up now" (A16, OQ-6). It cancels nothing else and
	// never closes Thunderbird; the backup goes on. nil = never. Ignored when
	// !Attended, and when it ended before S7 began.
	EndWait context.Context
}

// Result is one pre-sync's outcome, for the caller to surface as a warning.
type Result struct {
	Outcome Outcome
	// Side holds warn-class outcomes met on the way besides the main one (B6: an
	// unreadable state record while the session went on to sync; A9: a record that
	// could not be saved). The caller surfaces each as a run warning.
	Side []Outcome
	// Release, when non-nil, MUST be called by the caller AFTER the archive read: it
	// releases the per-profile lock that the pre-sync holds across the pre-sync and the
	// read, so a second scheduled run cannot pre-sync (and compact) this profile while
	// this run reads it (adversarial finding C2). app.Run defers it.
	Release func()

	// Verdict is the S7 verdict (ok, ok-noactivity, watch-truncated,
	// watch-truncated-noactivity or not-quiet), kept whatever the final outcome
	// became, so a scheduled owner-active or quit-* ending still says whether mail
	// arrived (§2.1, OQ-5). Zero when the session never reached a verdict.
	Verdict Outcome
	// Left is what the session left Thunderbird as (A8, C-23).
	Left Left
	// Started is when this session started Thunderbird; zero when it started none.
	Started time.Time
	// Waited runs from Thunderbird proven up to the quit request, or to the end of
	// the wait for mail (the verdict, a stopped wait or a cancel).
	Waited time.Duration
	// Minimized: Windows' backstop minimize succeeded (A14, C-56); false on macOS,
	// which starts Thunderbird truly out of sight.
	Minimized bool
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
	OutcomeStartBlocked.Code:        "Thunderbird could not be started: Windows would not let the program that ran this backup start it outside its own job, so the archive ran without a fresh sync. Do this: for a scheduled backup, click Repair on the Scheduled backup card; if you use Run now, start MailArchive Desktop from its shortcut (not from a terminal), then click Run now again.",
	OutcomeNoDesktopSession.Code:    "Thunderbird could not be started because there is no desktop session (the window server was unreachable). The archive ran anyway. Do this: run the sync while signed in to the Mac.",
	OutcomeDidNotStart.Code:         "Thunderbird did not appear within 90 seconds of being started, so the archive ran without a confirmed sync. Do this: run `mailarchive -thunderbird-sync` by hand to check it starts.",
	OutcomeIdentityUnavailable.Code: "The Thunderbird that was started could not be identified for certain, so it was not closed — nothing is ever closed on a guess. The archive ran anyway. Do this: Thunderbird may have been left open — quit it yourself when you are done.",
	OutcomeNotQuiet.Code:            "Thunderbird's mail files were still changing at the maximum wait (20 minutes), so the archive ran with the mail that had arrived by then. Some mail may still have been arriving. Do this: the next backup will pick it up, or run `mailarchive -thunderbird-sync` by hand if you need it archived now.",
	OutcomeExitedEarly.Code:         "Thunderbird closed before its mail files went quiet, so the sync may be incomplete. The archive ran anyway. Do this: the next scheduled run will pick up this mail, or run `mailarchive -thunderbird-sync` by hand if you need it archived now.",
	OutcomeQuitNotPermitted.Code:    "Thunderbird was left open because macOS did not allow Mail Archive to close it. Do this: in System Settings > Privacy & Security > Automation, allow Mail Archive to control Thunderbird; because the permission is asked per program, approve it from a run the scheduler starts (`launchctl kickstart gui/<uid>/<label>`), not only from Terminal. The archive ran anyway.",
	OutcomeQuitTimeout.Code:         "Thunderbird did not close within 2 minutes after it was asked to quit, so it was left open. Why: it may be showing a dialog (an unsent message, a password prompt) or still working. Do this: open Thunderbird and quit it, or run again once it is closed. The archive ran anyway.",
	OutcomeQuitSkippedOthers.Code:   "Thunderbird was left open because more than one Thunderbird was running and, on this system, closing it would have closed them all. Do this: quit the one the sync started when you are done. The archive ran anyway.",
	OutcomeQuitNoWindow.Code:        "Thunderbird was left open: its window could not be told apart from its other windows (for example a message being written), so MailArchive did not try to close it. Do this: quit Thunderbird from the taskbar or the notification area when you are done. The archive ran anyway.",
	OutcomeStateUnsaved.Code:        "The Thunderbird sync could not save its own state record, so if Thunderbird was left open the next run may not mention it. Do this: check the disk has free space and MailArchive's settings folder can be written to. The archive ran anyway.",
	OutcomeOwnerActive.Code:         "Thunderbird was left open because you seemed to be using it when the sync tried to close it. The archive ran anyway.",
	OutcomeSuccessorFound.Code:      "The Thunderbird that was started went away and a different one is now open; it was not touched. The archive ran anyway.",
	OutcomeLeftoverFound.Code:       "A Thunderbird an earlier backup started is still open, so this backup did not start another. Do this: quit that Thunderbird; it may be hidden or minimized, so quit it from its icon where your open programs are shown. The archive ran anyway.",
	OutcomeProfileUnusable.Code:     "No single, safe Thunderbird profile could be determined, so no sync ran. Do this: name the profile with `-thunderbird-profile PATH`. The archive ran anyway.",
	OutcomeAppNotFound.Code:         "Thunderbird was not found in its usual install location, so no sync ran. A Thunderbird installed in a non-standard place cannot be synced in this version. The archive ran anyway.",
	OutcomeRefusedElevated.Code:     "The Thunderbird sync was skipped because Mail Archive is running as root or an administrator — it would create profile files you could not then open. Do this: run it as your normal user. The archive ran anyway.",
	OutcomeStateUnreadable.Code:     "The pre-sync's own state record could not be read, so its recovery information was reset. The archive ran anyway.",
	OutcomeInternalError.Code:       "The Thunderbird pre-sync failed unexpectedly and was recovered; the archive ran anyway.",
	OutcomeNotSetUp.Code:            "The Thunderbird sync could not be prepared on this computer (its own settings folder could not be found), so the archive ran without it. Do this: repair the backup's schedule — in MailArchive Desktop, click Repair on the Scheduled backup card.",
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
//
// Corrected in design-presync-attended slice AS4 (§7, C-31, C-52, C-84): "or one you
// are using, is left alone" stopped being true once a Run now starts Thunderbird
// while the person is at the computer, so that sentence now says only an open
// Thunderbird is left alone, and that a scheduled backup waits until the person has
// been away. SyncNotice is the wording true on every platform and surface; where
// Run now starts Thunderbird (AttendedStartAllowed), SyncNoticeFor adds that a
// backup started with Run now does not wait for that.
const SyncNotice = syncNoticeHead + syncNoticeTail

const (
	syncNoticeHead   = "The Thunderbird sync starts Thunderbird in the background before the backup and waits for its mail to stop arriving, so the archive includes mail that had only reached the server. Because it drives your real Thunderbird, starting it does what opening Thunderbird always does: it checks all your accounts, sends anything waiting in your Outbox, runs your message filters, and may show notifications. When it then asks Thunderbird to quit, your accounts' on-exit actions run too (for example emptying Trash), so mail you deleted since the last backup may be removed before it is archived. A Thunderbird you already have open is left alone — never started or closed. A scheduled backup also waits until you have been away from the computer"
	syncNoticeRunNow = "; a backup you start with Run now does not wait for that, and leaves Thunderbird open for you to quit"
	syncNoticeTail   = ". If you use more than one Thunderbird profile, only the one holding this mailbox is synced."
)

// SyncNoticeFor is SyncNotice as a surface on goos shows it (§7, C-84): on a
// platform whose attended start is allowed (AttendedStartAllowed), it adds that a
// backup started with Run now does not wait until the person is away; elsewhere
// that clause is left out, since Run now does not start Thunderbird there.
func SyncNoticeFor(goos string) string {
	if AttendedStartAllowed(goos) {
		return syncNoticeHead + syncNoticeRunNow + syncNoticeTail
	}
	return SyncNotice
}

// MacAutomationNote is the macOS note shown beside the sync wherever it is offered on a
// Mac: the wizard's schedule question and MailArchive Desktop's Settings card both use
// this one constant (design-desktop-settings-card section 7, MA-861), so they cannot drift.
//
// Rewritten in design-presync-attended slice AS4 (§7, T10, C-52, C-62): it no longer
// tells the person to answer macOS's question from a Run now — a Run now never closes
// Thunderbird while macOS's attended close is not allowed (AttendedQuitAllowed), so it
// never raises the question — and it does not name the program macOS asks about
// before lab L-A2 says which it is. Slice AS9 brings the Run-now wording back, after
// L-A2 and macOS's attended-close flip.
const MacAutomationNote = "On a Mac, the first time a scheduled backup closes Thunderbird, macOS may ask for permission to control Thunderbird; until it is answered, each run leaves Thunderbird open. To answer, open System Settings > Privacy & Security > Automation and allow the entry listed there to control Thunderbird."

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
	// A13 (C-68): a truncated walk with nothing arriving is said as nothing new, never as done-and-synced.
	OutcomeWatchTruncatedNoActivity.Code: "Thunderbird sync: Thunderbird was started, but nothing new had arrived in 20 minutes (only part of its mail folders could be watched). Archiving what is on disk.",
	OutcomeWaitEnded.Code:                "Thunderbird sync: the wait for mail was stopped, as you asked; Thunderbird was left open. Archiving the mail that has arrived.",
}

// progressLine is the once-a-minute narration while the session waits for mail
// (A7): n whole minutes so far, of at most max.
func progressLine(n, max int) string {
	if n > max {
		n = max
	}
	return fmt.Sprintf("Thunderbird sync: still waiting for mail to arrive (%d min so far, up to %d)", n, max)
}

// narrate writes one owner-facing line to w when both are non-empty.
func narrate(w io.Writer, line string) {
	if w != nil && line != "" {
		_, _ = io.WriteString(w, line+"\n")
	}
}
