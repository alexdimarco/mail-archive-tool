package presync

import (
	"fmt"
	"runtime"
	"time"
)

// This file builds the end-of-run line (design-presync-attended §4.6, C-23, C-43,
// C-68): one past-tense sentence for how the session ended, then one for what it
// left Thunderbird as, in the words of the surface (the Desktop names buttons, the
// command line none) and of the platform (the Dock and "out of sight" on macOS, the
// taskbar and the notification area on Windows). SummaryLine stays the live log's
// present-tense line (§7). Slice AS1b builds the never-close lines and the
// no-change lines; slice AS-Q adds the attended close variants. Draft wording,
// settled by the friction review (§14 gate 3).

// lineWords are the surface and platform words a line is built from.
type lineWords struct {
	hidden   string // how Thunderbird was left: "out of sight", "minimized to the taskbar", or "" (Windows, never minimized)
	onScreen bool   // Windows, never minimized: its window may still be on screen (C-56, C-64)
	place    string // where to quit it from: "the Dock", "the taskbar or the notification area", or ""
	again    string // how to run the backup again
	next     string // the next run, by name
	desktop  bool
	windows  bool
}

func wordsFor(surface, goos string, minimized bool) lineWords {
	w := lineWords{again: "run the backup again", next: "the next backup"}
	if surface == SurfaceDesktop {
		w.desktop, w.again = true, "click Run now again"
	}
	switch goos {
	case "darwin":
		w.hidden, w.place = "out of sight", "the Dock"
	case "windows":
		w.windows, w.place = true, "the taskbar or the notification area"
		if minimized {
			w.hidden = "minimized to the taskbar"
		} else {
			w.onScreen = true
		}
	}
	return w
}

// leftOpen is "left open" with how it was left.
func (w lineWords) leftOpen() string {
	switch {
	case w.onScreen:
		return "left open; its window may still be on screen"
	case w.hidden != "":
		return "left open " + w.hidden
	}
	return "left open"
}

// quitIt is the instruction to quit it, from where.
func (w lineWords) quitIt() string {
	if w.place == "" {
		return "quit it"
	}
	return "quit it from " + w.place
}

// head is one outcome's own sentence. subject is how the second sentence refers
// to Thunderbird; noChange marks the two "nothing arrived" outcomes (C-68).
type head struct {
	text     func(w lineWords, waited string) string
	subject  string
	noChange bool
}

func fixed(s string) func(lineWords, string) string {
	return func(lineWords, string) string { return s }
}

const readHere = "; the backup read the mail already on this computer."

const noChangeText = "Thunderbird started, but nothing arrived in 20 minutes: either there was no new mail, or Thunderbird is waiting for a password or sign-in."

// outcomeHeads holds every outcome's own sentence but `cancelled`, whose line is
// built from what was left (§3.1). MA-897 fails for an outcome in allOutcomes that
// has none.
var outcomeHeads = map[Outcome]head{
	OutcomeOK: {text: func(_ lineWords, n string) string {
		return "Thunderbird synced: its mail stopped arriving after " + n + "."
	}, subject: "It"},
	OutcomeWatchTruncated: {text: func(_ lineWords, n string) string {
		return "Thunderbird synced: its mail stopped arriving after " + n + " (only part of its mail folders could be watched)."
	}, subject: "It"},
	OutcomeNotQuiet:                 {text: fixed("Thunderbird synced, but mail was still arriving at the 20-minute limit; the rest will be in the next backup."), subject: "It"},
	OutcomeNoActivity:               {text: fixed(noChangeText), subject: "It", noChange: true},
	OutcomeWatchTruncatedNoActivity: {text: fixed(noChangeText), subject: "It", noChange: true},
	OutcomeWaitEnded: {text: func(_ lineWords, n string) string {
		return "Thunderbird sync: you stopped the wait after " + n + ", so the backup read the mail that had arrived by then; mail still arriving will be in the next backup."
	}, subject: "Thunderbird"},
	OutcomeQuitNotPermitted: {text: func(w lineWords, _ string) string {
		if w.windows {
			return "Thunderbird synced, but it could not be asked to close."
		}
		return "Thunderbird synced, but macOS asked for permission to control Thunderbird, so it was not closed. Choose Allow when macOS asks."
	}, subject: "It"},
	OutcomeQuitTimeout:         {text: fixed("Thunderbird synced, but it did not close within 2 minutes after it was asked to."), subject: "It"},
	OutcomeQuitSkippedOthers:   {text: fixed("Thunderbird synced, but another Thunderbird was open, so it was not asked to close."), subject: "It"},
	OutcomeQuitNoWindow:        {text: fixed("Thunderbird synced, but its window could not be told apart from its other windows (for example a message being written), so it was not asked to close."), subject: "It"},
	OutcomeOwnerActive:         {text: fixed("Thunderbird synced, but you seemed to be using it, so it was not closed."), subject: "It"},
	OutcomeIdentityUnavailable: {text: fixed("Thunderbird synced, but the copy that was started could not be identified for certain, so it was not closed."), subject: "It"},
	OutcomeAlreadyRunning:      {text: fixed("Thunderbird was already open, so it was not started or closed" + readHere), subject: "It"},
	OutcomeOtherProfileRunning: {text: fixed("Thunderbird was not synced: it was open with another profile" + readHere), subject: "It"},
	OutcomeProfileBusy:         {text: fixed("Thunderbird was not synced: another backup was syncing it" + readHere), subject: "It"},
	OutcomeOwnerActiveSkipped:  {text: fixed("Thunderbird was not started because you were using the computer" + readHere), subject: "It"},
	OutcomeLeftoverFound: {text: func(w lineWords, _ string) string {
		where := "still open " + w.hidden
		if w.windows {
			where = "still open, minimized to the taskbar or in the notification area"
		} else if w.hidden == "" {
			where = "still open"
		}
		return "A Thunderbird an earlier backup started is " + where + ", so it was not synced again. " +
			capitalize(w.quitIt()) + ", then " + w.again + "."
	}, subject: "It"},
	OutcomeAppNotFound:     {text: fixed("Thunderbird was not synced: it was not found in its usual place" + readHere), subject: "It"},
	OutcomeRefusedElevated: {text: refusedElevated, subject: "It"},
	OutcomeProfileUnusable: {text: fixed("Thunderbird was not synced: its profile could not be used" + readHere), subject: "It"},
	OutcomeCannotStart:     {text: fixed("Thunderbird was not synced: it could not be started" + readHere), subject: "It"},
	OutcomeStartBlocked:    {text: startBlocked, subject: "It"},
	OutcomeDidNotStart:     {text: fixed("Thunderbird was not synced: it did not appear within 90 seconds of being started."), subject: "It"},
	OutcomeExitedEarly:     {text: fixed("Thunderbird closed before its mail settled, so the sync may be incomplete."), subject: "It"},
	OutcomeNoDesktopSession: {text: fixed("Thunderbird was not synced: no one was logged in, so it could not be started" + readHere),
		subject: "It"},
	OutcomeSuccessorFound: {text: fixed("Thunderbird was not synced: the copy that was started went away and a different one is open; it was not touched."), subject: "It"},
	OutcomeStateUnsaved:   {text: fixed("The Thunderbird sync could not save its own state record, so the next backup may not mention a Thunderbird still running."), subject: "Thunderbird"},
	OutcomeStateUnreadable: {text: fixed("The Thunderbird sync's own state record could not be read, so it was reset."),
		subject: "Thunderbird"},
	OutcomeInternalError: {text: fixed("Thunderbird was not synced: the sync failed unexpectedly; the backup ran anyway."), subject: "It"},
	OutcomeNotSetUp:      {text: fixed("Thunderbird was not synced: the sync could not be prepared on this computer" + readHere), subject: "It"},
}

func refusedElevated(w lineWords, _ string) string {
	if w.desktop {
		return "Thunderbird was not synced: MailArchive Desktop was started as administrator. Start MailArchive Desktop normally, not as administrator, then click Run now again."
	}
	return "Thunderbird was not synced: the backup ran as administrator. Run it as your normal user."
}

// startBlocked is start-blocked's line (§2.7, C-65): on the Desktop, how to start
// it so that it may start Thunderbird; at the command line, no button.
func startBlocked(w lineWords, _ string) string {
	if w.desktop {
		return "Thunderbird was not synced: MailArchive Desktop was started in a way that cannot start Thunderbird; start MailArchive Desktop from its shortcut, then click Run now again."
	}
	return "Thunderbird was not synced: Windows would not let this program start it" + readHere
}

// OutcomeLine is the owner-facing, past-tense line for how a session ended (§4.6):
// the outcome's own sentence, then what Thunderbird was left as (left), in the
// words of surface (SurfaceDesktop or SurfaceCLI) and goos ("" is
// runtime.GOOS). waited is the wait for mail; minimized is Windows' backstop
// minimize (false: its window may still be on screen). It names no path and no
// code.
func OutcomeLine(o Outcome, surface, goos string, left Left, waited time.Duration, minimized bool) string {
	if goos == "" {
		goos = runtime.GOOS
	}
	w := wordsFor(surface, goos, minimized)
	if o == OutcomeCancelled {
		return "Thunderbird sync: cancelled — " + cancelTail(w, left)
	}
	h, ok := outcomeHeads[o]
	if !ok {
		h = head{text: fixed("Thunderbird was not synced."), subject: "It"}
	}
	line := h.text(w, minutesText(waited))
	if tail := leftTail(w, left, h); tail != "" {
		line += " " + tail
	}
	return line
}

// leftTail is the sentence for what the session left Thunderbird as.
func leftTail(w lineWords, left Left, h head) string {
	if h.noChange {
		switch left {
		case LeftOpen, LeftOpenByRule, LeftOpenByChoice:
			s := "To check, open Thunderbird from " + w.place + "; it was left open."
			if w.place == "" {
				s = "To check, open Thunderbird; it was left open."
			}
			if w.onScreen {
				s += " Its window may still be on screen."
			}
			return s
		case LeftClosed:
			return "It was closed; to check, open Thunderbird and see whether it asks for a password or sign-in."
		}
	}
	switch left {
	case LeftClosed:
		return h.subject + " was closed."
	case LeftOpen:
		return "Thunderbird, started by this backup, was " + w.leftOpen() + ". " + capitalize(w.quitIt()) + " before " + w.next + "."
	case LeftOpenByRule:
		return h.subject + " was " + w.leftOpen() + "; " + w.quitIt() + " when you are done."
	case LeftOpenByChoice:
		return h.subject + " was left open, as you asked."
	case LeftMayBeStarting:
		return "Thunderbird may still be starting; " + w.quitIt() + "."
	}
	return ""
}

// cancelTail is a cancelled session's line by what it left (§3.1, C-23).
func cancelTail(w lineWords, left Left) string {
	switch left {
	case LeftClosed:
		return "Thunderbird, started by this backup, was closed."
	case LeftOpen:
		return "Thunderbird, started by this backup, was " + w.leftOpen() + ". " + capitalize(w.quitIt()) + " before " + w.next + "."
	case LeftOpenByRule:
		return "Thunderbird, started by this backup, was " + w.leftOpen() + "; " + w.quitIt() + " when you are done."
	case LeftOpenByChoice:
		return "Thunderbird was left open, as you asked."
	case LeftMayBeStarting:
		return "Thunderbird may still be starting; " + w.quitIt() + "."
	case LeftExited:
		// Started, then gone on its own before the Cancel reached it: never "not
		// started" (gate-2 pass 1 re-check).
		return "Thunderbird, started by this backup, was no longer running."
	}
	return "Thunderbird was not started."
}

// minutesText is a wait in whole minutes, as the line says it.
func minutesText(d time.Duration) string {
	m := int(d / time.Minute)
	if m < 1 {
		return "less than a minute"
	}
	return fmt.Sprintf("%d min", m)
}

func capitalize(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}

// endLine is the line runWith narrates when a session ends: an attended run is
// told with OutcomeLine, which knows what Thunderbird was left as; a scheduled run
// keeps SummaryLine, the live log's line (§7).
func endLine(cfg Config, r sessionResult) string {
	if cfg.Attended {
		return OutcomeLine(r.outcome, cfg.Surface, cfg.GOOS, r.left, r.waited, r.minimized)
	}
	return SummaryLine(r.outcome)
}

// profileNamedFor is whether a profile the Config names may skip the profiles.ini
// membership test (PC3): only when it is named and the run is NOT attended. An
// attended run's named profile must also be listed (C-18, accepted OD-4), so a
// hand-made job file cannot point the person's own click at an unlisted profile.
func profileNamedFor(cfg Config) bool { return cfg.ProfileNamed && !cfg.Attended }
