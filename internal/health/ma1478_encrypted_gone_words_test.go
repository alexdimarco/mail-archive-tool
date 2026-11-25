package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1478, R18, R37
// status and the Desktop's health view say an encrypted archive's gone mailbox in fixed sentences
// whose only variable is the date its record holds (docs/design-1.7.5-fixes.md §1.3): the WARN
// while the record's start minus mailbox_gone_since is under 14 days, the note after, never
// compared with now (the same 13- and 15-day cases as MA-1472); "the others were backed up" only
// after a run that ended ok. Positive twin first: an ok record 13 days on WARNs in the full
// sentence.
func TestStatusSaysAnEncryptedArchivesGoneMailboxInFixedWords(t *testing.T) {
	since := time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC)
	date := since.Local().Format("Jan 2, 2006")
	report := func(status string, started, now time.Time, gone bool, at time.Time) (string, Report) {
		in := Input{Out: t.TempDir(), Encrypted: true, EncryptedWhy: "test", LastRunState: state.LastRunPresent,
			LastRun: state.LastRun{Status: status, Started: started, Finished: started.Add(time.Minute), Encrypted: true, MailboxGone: gone, MailboxGoneSince: at, MailboxGoneCount: 1}}
		rep := Assess(in, now)
		return strings.Join(rep.Reasons, "\n"), rep
	}
	day13, day15 := since.Add(13*24*time.Hour), since.Add(15*24*time.Hour)
	got, rep := report(state.RunOK, day13, day13, true, since)
	if !strings.Contains(got, "WARN: "+state.MailboxGoneText(date)) || !rep.has(state.MailboxGoneCode) {
		t.Fatalf("positive twin: an ok record 13 days on WARNs in the fixed sentence:\n%s", got)
	}
	if got, _ := report(state.RunOK, day13, day13.Add(365*24*time.Hour), true, since); !strings.Contains(got, "WARN: "+state.MailboxGoneText(date)) {
		t.Fatalf("13 days by the record, read a year later, is still a WARN:\n%s", got)
	}
	for _, now := range []time.Time{day15, day15.Add(365 * 24 * time.Hour)} {
		got, rep := report(state.RunOK, day15, now, true, since)
		if !strings.Contains(got, state.MailboxGoneNoteText(date)) || strings.Contains(got, "WARN: a mailbox") || !rep.has("mailbox_gone_note") {
			t.Fatalf("15 days by the record is the note:\n%s", got)
		}
	}
	for _, status := range []string{state.RunFailed, state.RunCancelled, state.RunRunning} {
		got, _ := report(status, day13, day13, true, since)
		if !strings.Contains(got, "WARN: "+state.MailboxGoneShortText(date)) || strings.Contains(got, "the others were backed up") {
			t.Fatalf("a %s record says the short sentence:\n%s", status, got)
		}
	}
	if got, rep := report(state.RunOK, day13, day13, false, time.Time{}); rep.has(state.MailboxGoneCode) || rep.has("mailbox_gone_note") || strings.Contains(got, "has not been found") {
		t.Fatalf("a record that skipped nothing says nothing of it:\n%s", got)
	}
	for _, s := range []string{state.MailboxGoneText(date), state.MailboxGoneShortText(date), state.MailboxGoneNoteText(date)} {
		if strings.ContainsAny(strings.ReplaceAll(s, date, ""), "/\\%") {
			t.Fatalf("a fixed sentence names no path: %q", s)
		}
	}
}
