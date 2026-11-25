package health

import (
	"time"

	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/state"
)

// The later words for a Thunderbird account's stop (docs/design-per-mailbox-archives.md §11 "The
// later words for both stops", revision 2.1's F-03; MA-1506). A run stopped because Thunderbird keeps
// the account's mail in another folder now (moved: C2, F-08 and 1.7.5's moved stop, MA-1481, MA-1504)
// or no longer has the account (removed: C13) keeps a fixed reason code in its plaintext refused
// record (state.LastRun.SourceReason). After 3 days status and the Your-archives banner, and at 30
// days the pause, would say "reconnect the source … click Repair", a dead end for these archives:
// the folder may be there, and Repair says no. So they say the reason's words instead. A record with
// no reason keeps today's words. The design's draft words, for the friction review.

// SourceReasonText is what status and the Your-archives banner say, in place of "reconnect the
// source", for a source-lost archive whose last refused run kept the reason reason, since the
// source streak's first date since: "" for no reason, an unknown one, or no date (today's words).
func SourceReasonText(reason string, since time.Time) string {
	if since.IsZero() {
		return ""
	}
	date := since.Format("Jan 2, 2006")
	switch reason {
	case state.SourceReasonMoved:
		// The friction review's F12: the run's own error's second way, "or choose One archive for each
		// mailbox and the same folder as before", is said here too.
		return "Thunderbird stopped using this archive's mail folder on " + date + ": its account now keeps its mail in another folder. The mail saved so far stays here. To back that account up, click Make a new archive… and choose it again (or choose One archive for each mailbox and the same folder as before); then click Pause (make read-only) on this archive."
	case state.SourceReasonRemoved:
		return "Thunderbird no longer has the account this archive backs up (since " + date + "). The mail saved so far stays here. To stop its backups, click Pause (make read-only)."
	}
	return ""
}

// SourceReasonPauseText is the 30-day pause's words (its email's reason and the run log's line) in
// place of "reconnect the source, then … click Repair", for an archive whose last refused run kept
// the reason reason: "" for no reason, an unknown one, or no date (today's words).
func SourceReasonPauseText(reason string, since time.Time) string {
	if since.IsZero() {
		return ""
	}
	date := since.Format("Jan 2, 2006")
	// These go out in an email, where "here" points nowhere (the friction review's F12): "Its mail
	// stays in the archive." ("Its mail stays here." before).
	switch reason {
	case state.SourceReasonMoved:
		return "MailArchive paused this archive's scheduled backup: Thunderbird has not used its mail folder since " + date + ". Its mail stays in the archive. To back that account up, click Make a new archive… and choose it again."
	case state.SourceReasonRemoved:
		return "MailArchive paused this archive's scheduled backup: Thunderbird has not had its account since " + date + ". Its mail stays in the archive."
	}
	return ""
}

// SourceReasonAt is the reason the plaintext last-run record of the archive at out keeps (read
// bounded, never through a link, and only as one of the closed set: state.ReadLastRun): "" for an
// encrypted archive, a record with none, or no record that can be read.
func SourceReasonAt(out string) string {
	if out == "" || encmark.MustMinimise(out) {
		return ""
	}
	lr, st, _ := state.ReadLastRun(out)
	if st != state.LastRunPresent || lr.Encrypted {
		return ""
	}
	return lr.SourceReason
}

// SourceReasonWordsAt is SourceReasonText for the archive at out, from its record and its source
// streak, when the streak is loud (3 days, SurfaceLoudly), else "": the Your-archives banner asks
// it, and status says the same from the same two records (X10).
func SourceReasonWordsAt(out string) string {
	s, err := state.ReadSourceStreak(out)
	if err != nil || !s.SurfaceLoudly() {
		return ""
	}
	return SourceReasonText(SourceReasonAt(out), s.FirstUnreachable)
}

// SourceReasonPauseWordsAt is SourceReasonPauseText for the archive at out, from its record and its
// source streak's first date: the 30-day pause asks it.
func SourceReasonPauseWordsAt(out string) string {
	s, err := state.ReadSourceStreak(out)
	if err != nil {
		return ""
	}
	return SourceReasonPauseText(SourceReasonAt(out), s.FirstUnreachable)
}
