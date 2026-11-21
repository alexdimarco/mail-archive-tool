package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// covers: MA-1472, MA-1478, R18
// One backup whose clock reads behind never moves a mailbox's first-found date earlier (the 1.7.5
// adversarial pass's CON-02; the owner, 2026-11-20, Q8: "Yes, change it (Recommended)"): a recorded
// date later than this run's start is kept as recorded, so once the clock is right again status
// still WARNs for the days left, and its "on or after" day is counted from that date. The same for
// an encrypted archive's one date. The skeptic's sequence: first found at T; one run with the clock
// at T minus 10 days; then a run at T plus 4 days. Positive twin first: at T plus 15 days, a note.
func TestAClockBehindNeverMovesTheFirstFoundDateEarlier(t *testing.T) {
	home := tmpDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	parent := filepath.Join(home, ".thunderbird", "abcd1234.default-release", "ImapMail")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(parent, "imap.qzb.example") // gone
	first := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	behind, right, later := first.Add(-10*24*time.Hour), first.Add(4*24*time.Hour), first.Add(15*24*time.Hour)
	status := func(lr state.LastRun) string {
		return strings.Join(health.Assess(health.Input{Out: tmpDir(t), Encrypted: lr.Encrypted, LastRunState: state.LastRunPresent, LastRun: lr}, lr.Started).Reasons, "\n")
	}
	noteDay := first.Add(state.MailboxGoneNoteAfter).Local().Format("Jan 2, 2006")

	// Plaintext.
	out := tmpDir(t)
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: first, Finished: first,
		Warnings: []state.RunWarning{{Code: state.MailboxGoneCode, Message: "x", Path: b, Since: first}}}); err != nil {
		t.Fatal(err)
	}
	s := []SkippedInput{{Path: b}}
	resolveGoneSince(out, s, behind)
	if !s[0].Since.Equal(first) {
		t.Errorf("a run whose clock is behind moved the first-found date from %s to %s", first, s[0].Since)
	}
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: behind, Finished: behind, Warnings: goneWarnings(s)}); err != nil {
		t.Fatal(err)
	}
	if got := status(state.LastRun{Status: state.RunOK, Started: behind, Finished: behind, Warnings: goneWarnings(s)}); !strings.Contains(got, "on or after "+noteDay+" this is only a note") {
		t.Errorf("the record of the run whose clock is behind names its note day from the first-found date (%s):\n%s", noteDay, got)
	}
	for _, c := range []struct {
		at   time.Time
		warn bool
	}{{later, false}, {right, true}} { // positive twin first
		s := []SkippedInput{{Path: b}}
		resolveGoneSince(out, s, c.at)
		got := status(state.LastRun{Status: state.RunOK, Started: c.at, Finished: c.at, Warnings: goneWarnings(s)})
		if c.warn != strings.Contains(got, "WARN: a mailbox this archive backs up") {
			t.Errorf("plaintext, %s after the first-found date (carried as %s): WARN %v wanted:\n%s", c.at.Sub(first), s[0].Since, c.warn, got)
		}
	}

	// Encrypted: one date for the archive, through the same clamp.
	enc := tmpDir(t)
	plantEncryptedGone(t, enc, first, 1)
	e := []SkippedInput{{Path: b}}
	resolveEncryptedGone(enc, e, behind)
	if !e[0].Since.Equal(first) {
		t.Errorf("encrypted: a run whose clock is behind moved the first-found date from %s to %s", first, e[0].Since)
	}
	plantEncryptedGone(t, enc, e[0].Since, 1)
	for _, c := range []struct {
		at   time.Time
		warn bool
	}{{later, false}, {right, true}} {
		e := []SkippedInput{{Path: b}}
		resolveEncryptedGone(enc, e, c.at)
		got := status(state.LastRun{Status: state.RunOK, Started: c.at, Finished: c.at, Encrypted: true, MailboxGone: true, MailboxGoneSince: e[0].Since, MailboxGoneCount: 1})
		if c.warn != strings.Contains(got, "WARN: a mailbox this archive backs up") {
			t.Errorf("encrypted, %s after the first-found date (carried as %s): WARN %v wanted:\n%s", c.at.Sub(first), e[0].Since, c.warn, got)
		}
	}
}
