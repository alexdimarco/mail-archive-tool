package health

// The Gmail lines of a status (docs/design-gmail-dedup.md §8.1, the B3 friction review's F8, and
// the B3 UX walk's U3, U4, U7 and U9): one source for `status`'s text, `status -json` (as reasons
// with codes) and the Desktop's Status & health view. Neither changes the posture.

import (
	"slices"
	"strings"

	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// gmailWaiting is the Gmail stores of the plaintext archive at out whose question waits: those
// the last backup that ended ok found off, waiting (gmail_off) or held back (gmail_held: a Not now
// in that run, or an unanswered terminal question, U4), unless this computer keeps a Not now for
// the archive, a yes for the store, or the loaded manifest already has the store switched on
// (on). Gather calls it only when the manifest loaded, so an archive at a newer version (another
// computer tidying it) never gets the line (U3).
func gmailWaiting(out string, lr state.LastRun, st state.LastRunState, on func(string) bool) []string {
	if st != state.LastRunPresent || lr.Encrypted || lr.Status != state.RunOK {
		return nil
	}
	if len(lr.GmailOff) == 0 && len(lr.GmailHeld) == 0 {
		return nil
	}
	kept := gmailanswer.Read(out)
	if kept.NotNow {
		return nil
	}
	var waiting []string
	for _, t := range append(append([]string(nil), lr.GmailOff...), lr.GmailHeld...) {
		if state.SafeToken(t) && !on(t) && !slices.Contains(kept.Yes, t) && !slices.Contains(waiting, t) {
			waiting = append(waiting, t)
		}
	}
	return waiting
}

// shownStore is a store name as a status line shows it: control characters and bidirectional
// overrides taken out (the B3 adversarial pass's A5).
func shownStore(t string) string {
	return util.StripBidi(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return ' '
		}
		return r
	}, t))
}

// gmailNotes adds the Gmail lines to r: the count line with the 1.8 line (U9), and one waiting
// line per store (F8), each a note with its code (gmail_counts, gmail_waiting).
func (r *Report) gmailNotes(in Input) {
	if in.Encrypted {
		return
	}
	if l := in.GmailCounts.Line(); l != "" {
		r.note("gmail_counts", l+" "+state.GmailNeedsLine)
	}
	for _, t := range in.GmailWaiting {
		r.note("gmail_waiting", state.GmailWaitingLine(shownStore(t)))
	}
}
