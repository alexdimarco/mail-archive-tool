package health

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// goneReport is status's report of a plaintext archive whose last run (started at started, ended
// status) recorded w, assessed at now.
func goneReport(t *testing.T, status string, started time.Time, w state.RunWarning, now time.Time) (string, Report) {
	t.Helper()
	in := Input{Out: t.TempDir(), LastRunState: state.LastRunPresent, LastRun: state.LastRun{Status: status, Started: started, Finished: started.Add(time.Minute), Warnings: []state.RunWarning{w}}}
	rep := Assess(in, now)
	return strings.Join(rep.Reasons, "\n"), rep
}

// covers: MA-1472, R18
// status and the Desktop's health view (one producer, X10) say a plaintext record's mailbox_gone
// warning in their own words, built from Path, Name and Since, never from the recorded Message
// (docs/design-1.7.5-fixes.md §1.3): a WARN while the last run's start minus Since is under 14 days
// (the owner, 2026-11-20, Q2), a note after, never compared with now; "the other mailboxes are
// still backed up" only when the record's status is ok; the note's remedy names the Yes to keeping
// it current; a Since that is zero, before MinPlausibleTime or after the run's start counts as no
// time, so the WARN shows. Positive twin first: an ok record 13 days on says the WARN with the
// clause.
func TestStatusSaysAGoneMailboxForFourteenDaysOfBackupsThenANote(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(home, ".thunderbird", "abcd1234.default-release", "ImapMail", "imap.qzb.example")
	since := time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC)
	w := state.RunWarning{Code: state.MailboxGoneCode, Message: "PLANTED MESSAGE", Path: path, Name: "qzsam@example.com", Since: since}
	// Amended 2026-11-20 by the 1.7.5 adversarial pass (CON-01): the day named is the first local day
	// every backup of which is 14 days or more after since (it was since plus 14 days, whose own day
	// could still WARN), worked out here on its own: the day of since+14d, or the next day when
	// since+14d is not at local midnight. The whole WARN stays pinned word for word.
	firstNoteDay := func(s time.Time) string {
		x := s.Add(14 * 24 * time.Hour).Local()
		if x.Hour() != 0 || x.Minute() != 0 || x.Second() != 0 || x.Nanosecond() != 0 {
			x = x.AddDate(0, 0, 1)
		}
		return x.Format("Jan 2, 2006")
	}
	date, after := since.Local().Format("Jan 2, 2006"), firstNoteDay(since)
	warn := "WARN: a mailbox this archive backs up is no longer on this computer: qzsam@example.com (" + path + "), not found since " + date + ". Its mail already saved stays in the archive, and the other mailboxes are still backed up. If you removed it on purpose, nothing needs doing: from the first backup on or after " + after + " this is only a note. If you didn't remove it, add the account back in Thunderbird and the next backup finds it again."

	day13, day15 := since.Add(13*24*time.Hour), since.Add(15*24*time.Hour)
	got, rep := goneReport(t, state.RunOK, day13, w, day13.Add(time.Hour))
	if !strings.Contains(got, warn) || !rep.has(state.MailboxGoneCode) || rep.has("mailbox_gone_note") {
		t.Fatalf("positive twin: an ok record 13 days on WARNs:\n%s\nwant\n%s", got, warn)
	}
	if strings.Contains(got, "PLANTED") {
		t.Fatalf("the recorded Message is never shown: %s", got)
	}
	// Never compared with now: read a year later it is still a WARN.
	if got, _ := goneReport(t, state.RunOK, day13, w, day13.Add(365*24*time.Hour)); !strings.Contains(got, warn) {
		t.Fatalf("13 days by the record, read a year later, is still a WARN:\n%s", got)
	}
	// 15 days by the record: a note, whatever now is.
	// Amended 2026-11-20 by the 1.7.5 friction review (F11): the note names the button that ends it;
	// it said "set the archive up again in the wizard, with the same archive folder, choose All of
	// them, and answer Yes to keeping it current." Pinned word for word as before.
	note := "a mailbox this archive backed up is no longer on this computer: qzsam@example.com (" + path + "), not found since " + date + ". Its mail already saved stays in the archive. This line stays while the archive's backup lists that mailbox; to end it, click Make a new archive… in MailArchive Desktop, choose All of them and this same archive folder, and answer Yes to keeping it current."
	for _, now := range []time.Time{day15, day15.Add(365 * 24 * time.Hour)} {
		got, rep := goneReport(t, state.RunOK, day15, w, now)
		if !strings.Contains(got, "\n"+note) || strings.Contains(got, "WARN: a mailbox") || rep.has(state.MailboxGoneCode) || !rep.has("mailbox_gone_note") {
			t.Fatalf("15 days by the record is a note, with no WARN of its own:\n%s", got)
		}
	}
	// Only a run that ended ok says the others are still backed up.
	short := "not found since " + date + ". Its mail already saved stays in the archive. If you removed it on purpose"
	for _, status := range []string{state.RunCancelled, state.RunFailed, state.RunRunning} {
		got, _ := goneReport(t, status, day13, w, day13.Add(time.Hour))
		if !strings.Contains(got, short) || strings.Contains(got, "still backed up") {
			t.Fatalf("a %s record must not say the others are still backed up:\n%s", status, got)
		}
	}
	// A time that counts as no time: the WARN shows, dated from the run's start.
	// Amended 2026-11-20 by the 1.7.5 adversarial pass (CON-02; the owner, 2026-11-20, Q8: "Yes,
	// change it (Recommended)"): a since after the run's start still WARNs, but is now said as
	// recorded, and its "on or after" day counted from it, since that is when the WARN ends. That
	// case is asserted below at the same strength (the WARN, and its own two dates); the zero and
	// 2020 cases stay here unchanged.
	for _, bad := range []time.Time{{}, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)} {
		v := w
		v.Since = bad
		if got, rep := goneReport(t, state.RunOK, day15, v, day15); !rep.has(state.MailboxGoneCode) || !strings.Contains(got, "not found since "+day15.Local().Format("Jan 2, 2006")) {
			t.Fatalf("a since of %s counts as no time, so the WARN shows:\n%s", bad, got)
		}
	}
	ahead := w
	ahead.Since = day15.Add(30 * 24 * time.Hour)
	if got, rep := goneReport(t, state.RunOK, day15, ahead, day15); !rep.has(state.MailboxGoneCode) || rep.has("mailbox_gone_note") ||
		!strings.Contains(got, "not found since "+ahead.Since.Local().Format("Jan 2, 2006")) ||
		!strings.Contains(got, "on or after "+firstNoteDay(ahead.Since)) {
		t.Fatalf("a since after the run's start WARNs, dated as recorded (Q8):\n%s", got)
	}
	// No name: the path alone; a place another program keeps names that program.
	v := w
	v.Name, v.Path = "", filepath.Join(home, ".cache", "evolution", "mail", "1234abcd")
	if got, _ := goneReport(t, state.RunOK, day13, v, day13); !strings.Contains(got, "no longer on this computer: "+v.Path+", not found") || !strings.Contains(got, "add the account back in Evolution") {
		t.Fatalf("no name, in Evolution's place:\n%s", got)
	}
}
