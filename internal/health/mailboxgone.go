package health

import (
	"fmt"
	"time"

	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/state"
)

// A mailbox a combined archive backs up that is gone (docs/design-1.7.5-fixes.md §1.3, MA-1472):
// status and the Desktop's health view build the words from the record's mailbox_gone warning's
// Path, Name and Since, never from its Message.

// mailboxGoneDate is how status says the day of t.
const mailboxGoneDate = "Jan 2, 2006"

// goneSinceKnown reports whether since is a time the record can count from: not zero, not before
// state.MinPlausibleTime (a clock that was not set), and not after the last run's start (a
// planted or damaged record). Any other counts as no time at all, so the WARN shows.
func goneSinceKnown(since, started time.Time) bool {
	return !since.IsZero() && !since.Before(state.MinPlausibleTime) && !since.After(started)
}

// mailboxGoneWarns reports whether a mailbox first found gone at since is still a WARN in a record
// whose last run started at started: while started minus since is under
// state.MailboxGoneNoteAfter (the owner's 14 days, 2026-11-20, Q2), counted between backups and
// never compared with now.
func mailboxGoneWarns(since, started time.Time) bool {
	return !goneSinceKnown(since, started) || started.Sub(since) < state.MailboxGoneNoteAfter
}

// addBackIn is the WARN's last sentence's remedy for a mailbox kept where auto-discovery looks for
// program's mail.
func addBackIn(path string) string {
	switch p := autoplace.ProgramOf(path); p {
	case autoplace.Outlook, autoplace.Evolution, autoplace.Thunderbird:
		return "add the account back in " + p
	}
	return "put it back where it was"
}

// encryptedMailboxGone says an encrypted record's gone mailbox (MA-1478) in fixed sentences: the
// WARN while it is under 14 days old by the record's own start ("the others were backed up" only
// after a run that ended ok), the note after.
func encryptedMailboxGone(r *Report, lr state.LastRun) {
	since := lr.MailboxGoneSince
	if !goneSinceKnown(since, lr.Started) {
		since = lr.Started
	}
	date := since.Local().Format(mailboxGoneDate)
	switch {
	case !mailboxGoneWarns(lr.MailboxGoneSince, lr.Started):
		r.note("mailbox_gone_note", state.MailboxGoneNoteText(date))
	case lr.Status == state.RunOK:
		r.warn(state.MailboxGoneCode, state.MailboxGoneText(date))
	default:
		r.warn(state.MailboxGoneCode, state.MailboxGoneShortText(date))
	}
}

// mailboxGone says one mailbox_gone warning of the plaintext record lr: a WARN while it is under
// 14 days old by the record's own start, a note after.
func mailboxGone(r *Report, lr state.LastRun, w state.RunWarning) {
	label := w.Path
	if w.Name != "" {
		label = w.Name + " (" + w.Path + ")"
	}
	since := w.Since
	if !goneSinceKnown(since, lr.Started) {
		since = lr.Started
	}
	date := since.Local().Format(mailboxGoneDate)
	if !mailboxGoneWarns(w.Since, lr.Started) {
		r.note("mailbox_gone_note", fmt.Sprintf("a mailbox this archive backed up is no longer on this computer: %s, not found since %s. Its mail already saved stays in the archive. This line stays while the archive's backup lists that mailbox; to end it, set the archive up again in the wizard, with the same archive folder, choose All of them, and answer Yes to keeping it current.", label, date))
		return
	}
	// "The other mailboxes are still backed up" only after a run that ended ok: the warning reaches
	// the running, cancelled and failed records too, whose own outcome is said beside it (R7).
	kept := "Its mail already saved stays in the archive."
	if lr.Status == state.RunOK {
		kept = "Its mail already saved stays in the archive, and the other mailboxes are still backed up."
	}
	note := since.Add(state.MailboxGoneNoteAfter).Local().Format(mailboxGoneDate)
	r.warn(state.MailboxGoneCode, fmt.Sprintf("a mailbox this archive backs up is no longer on this computer: %s, not found since %s. %s If you removed it on purpose, nothing needs doing: from the first backup on or after %s this is only a note. If you didn't remove it, %s and the next backup finds it again.", label, date, kept, note, addBackIn(w.Path)))
}
