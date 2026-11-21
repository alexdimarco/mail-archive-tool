package health

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/encmark"
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

// goneDay is the first-found time status says the dates from: since as recorded, or the record's
// own start for a since that is zero or before state.MinPlausibleTime (a clock that was not set).
// Amended by the 1.7.5 adversarial pass (CON-02; the owner, 2026-11-20, Q8): a since after the
// run's start is said as recorded, so the "on or after" day is the day the WARN really ends (it
// WARNs until the run's start is 14 days past since); it was the run's start before.
func goneDay(since, started time.Time) time.Time {
	if since.IsZero() || since.Before(state.MinPlausibleTime) {
		return started
	}
	return since
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

// goneRemedy is the WARN's last sentence for the gone mailbox w (the 1.7.5 friction review's F4 and
// F9, beside addBackIn's words, which stay for the rest): an account Thunderbird still listed when
// the run looked is told to let Thunderbird download its mail; an Outlook data file (.pst) moved
// away is told to put it back in its folder or choose it in its new place; any other, "If you
// didn't remove it, " and addBackIn's words.
func goneRemedy(w state.RunWarning) string {
	switch {
	case w.Listed && autoplace.ProgramOf(w.Path) == autoplace.Thunderbird:
		return "Thunderbird still lists this account: open Thunderbird and let it download the account's mail; the next backup finds it again."
	case autoplace.ProgramOf(w.Path) == autoplace.Outlook && strings.EqualFold(filepath.Ext(w.Path), ".pst"):
		return "If you moved this file, put it back in " + filepath.Dir(w.Path) + ", or click Make a new archive… in MailArchive Desktop and choose it in its new place."
	}
	return "If you didn't remove it, " + addBackIn(w.Path) + " and the next backup finds it again."
}

// noteDay is the day the WARN names as the first on which a backup is only a note (the 1.7.5
// adversarial pass's CON-01): the first local day every moment of which is MailboxGoneNoteAfter or
// more after since, so a backup on that day is never still a WARN. since plus 14 days falling at
// local midnight names that day; otherwise the next.
func noteDay(since time.Time) time.Time {
	t := since.Add(state.MailboxGoneNoteAfter).Local()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	if t.After(day) {
		day = day.AddDate(0, 0, 1)
	}
	return day
}

// othersKept is the WARN's clause for what is still backed up, after a run that ended ok (the 1.7.5
// friction review's F12): "Local Folders" when the only input the job lists that no gone warning
// names is a profile's Local Folders, else "the other mailboxes". Read from the record alone.
func othersKept(lr state.LastRun) string {
	_, inputs := jobInputs(lr.Job)
	gone := map[string]bool{}
	for _, w := range lr.Warnings {
		if w.Code == state.MailboxGoneCode {
			gone[w.Path] = true
		}
	}
	var left []string
	for _, in := range inputs {
		if !gone[in] {
			left = append(left, in)
		}
	}
	if len(left) == 1 && filepath.Base(left[0]) == "Local Folders" {
		return "Local Folders is still backed up"
	}
	return "the other mailboxes are still backed up"
}

// GoneMailboxRepairable reports whether a Repair or a Run now would now get past the gone mailbox
// an archive's last run refused on (docs/design-1.7.5-fixes.md §1.3, "Archives 1.7.4 already
// stopped"; MA-1479): status, the Your-archives banner and the Settings card all ask it, so they
// say Repair only where it works (X10). Yes only when all hold:
//
//   - the archive is plaintext: its record is not an encrypted one, and encmark.MustMinimise says
//     no;
//   - its source streak's last kind is local-source-unreachable;
//   - its last-run record (read bounded, never through a link, cleaned) is a failed run with -auto
//     in its job, and every -input the job lists is a plain local path (autoplace.NonLocal says
//     no), so no path the record names is looked at unless all are local (MA-869, OUT-01);
//   - its error is exactly autoplace.GoneText of one of the job's listed -inputs, and the run's own
//     rule (autoplace.GoneSkippable) accepts that input now;
//   - no other listed input would refuse the run: each is there (os.Stat, as the run checks) or
//     GoneSkippable accepts it, and at least one is there (a store only auto-discovery would find
//     is not looked for, so then the answer is no).
//
// Any other archive keeps today's words.
func GoneMailboxRepairable(out string) bool {
	if out == "" || encmark.MustMinimise(out) {
		return false
	}
	if s, err := state.ReadSourceStreak(out); err != nil || s.LastKind != "local-source-unreachable" {
		return false
	}
	lr, st, _ := state.ReadLastRun(out)
	if st != state.LastRunPresent || lr.Encrypted || lr.Status != state.RunFailed {
		return false
	}
	auto, inputs := jobInputs(lr.Job)
	if !auto {
		return false
	}
	// The record is untrusted (C-17): a listed path that is not a plain local one (a network or
	// device path, or one not absolute) is never looked at, so the answer is no before any file
	// check (MA-869; the 1.7.5 adversarial pass's OUT-01 and INS-01).
	for _, in := range inputs {
		if autoplace.NonLocal(in) {
			return false
		}
	}
	named := ""
	for _, in := range inputs {
		if lr.Error == autoplace.GoneText(in) {
			named = in
			break
		}
	}
	if named == "" || !autoplace.GoneSkippable(named) {
		return false
	}
	there := 0
	for _, in := range inputs {
		if _, err := autoplace.Stat(in); err == nil {
			there++
		} else if !autoplace.GoneSkippable(in) {
			return false
		}
	}
	return there > 0
}

// jobInputs reads a recorded job's -auto and its listed -inputs, in order.
func jobInputs(job []string) (auto bool, inputs []string) {
	for i := 0; i < len(job); i++ {
		switch job[i] {
		case "-auto":
			auto = true
		case "-input":
			if i+1 < len(job) {
				inputs = append(inputs, job[i+1])
				i++
			}
		}
	}
	return auto, inputs
}

// encryptedMailboxGone says an encrypted record's gone mailbox (MA-1478) in fixed sentences: the
// WARN while it is under 14 days old by the record's own start ("the others were backed up" only
// after a run that ended ok), the note after.
func encryptedMailboxGone(r *Report, lr state.LastRun) {
	date := goneDay(lr.MailboxGoneSince, lr.Started).Local().Format(mailboxGoneDate)
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
	// A warning with no path (a 1.7.4 program's write-back drops it, or a damaged record) is said in
	// a fixed sentence, never a broken one (the 1.7.5 adversarial pass's CON-03).
	if w.Path == "" {
		r.warn(state.MailboxGoneCode, "a mailbox this archive backs up was not found, so it was skipped; its record does not say which. Click Run now in MailArchive Desktop to see which one.")
		return
	}
	label := w.Path
	if w.Name != "" {
		label = w.Name + " (" + w.Path + ")"
	}
	since := goneDay(w.Since, lr.Started)
	date := since.Local().Format(mailboxGoneDate)
	if !mailboxGoneWarns(w.Since, lr.Started) {
		// The friction review's F11: the note names the button that ends it (it said "set the
		// archive up again in the wizard").
		r.note("mailbox_gone_note", fmt.Sprintf("a mailbox this archive backed up is no longer on this computer: %s, not found since %s. Its mail already saved stays in the archive. This line stays while the archive's backup lists that mailbox; to end it, click Make a new archive… in MailArchive Desktop, choose All of them and this same archive folder, and answer Yes to keeping it current.", label, date))
		return
	}
	// "The other mailboxes are still backed up" only after a run that ended ok: the warning reaches
	// the running, cancelled and failed records too, whose own outcome is said beside it (R7);
	// "Local Folders" when it alone is left (F12).
	kept := "Its mail already saved stays in the archive."
	if lr.Status == state.RunOK {
		kept = "Its mail already saved stays in the archive, and " + othersKept(lr) + "."
	}
	note := noteDay(since).Format(mailboxGoneDate)
	r.warn(state.MailboxGoneCode, fmt.Sprintf("a mailbox this archive backs up is no longer on this computer: %s, not found since %s. %s If you removed it on purpose, nothing needs doing: from the first backup on or after %s this is only a note. %s", label, date, kept, note, goneRemedy(w)))
}
