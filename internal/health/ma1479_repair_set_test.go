package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-1479, MA-693, R18
// Status after Repair (the 1.7.5 friction review's F2 and F10): for an archive the Repair helper
// says yes to, whose scheduled entry is there again, the source_lost WARN names the missing mailbox
// and says the next backup skips it and Run now backs up at once, no longer "click Repair". Positive
// twin first: with its entry gone (auto-paused), it says Repair, naming the mailbox.
func TestStatusSaysTheNextBackupSkipsTheMissingMailboxOnceItsEntryIsBack(t *testing.T) {
	out := t.TempDir()
	t0 := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	if err := state.WriteSourceStreak(out, state.SourceStreak{ConsecutiveFails: 31, FirstUnreachable: t0, LastUnreachable: t0.Add(31 * 24 * time.Hour), LastKind: "local-source-unreachable"}); err != nil {
		t.Fatal(err)
	}
	named := "/qz/.thunderbird/p/ImapMail/imap.qzb.example"
	in := Input{Out: out, HasDescriptor: true, Desc: schedule.Descriptor{Name: "mailarchive-qz", Interval: "daily", At: "03:00"}, GoneRepair: &named}
	say := func(st schedule.State) string {
		in.SchedState = st
		return strings.Join(Assess(in, t0.Add(32*24*time.Hour)).Reasons, "\n")
	}
	if got := say(schedule.NotInstalled); !strings.Contains(got, "(the missing mailbox: "+named+") — if a mail account was removed, click Repair (or Run now)") {
		t.Fatalf("positive twin: auto-paused, status says Repair, naming the mailbox:\n%s", got)
	}
	got := say(schedule.Installed)
	if !strings.Contains(got, "(the missing mailbox: "+named+") — its scheduled backup is set, and MailArchive now skips a mailbox that is gone, so the next backup backs up the others; to back up now, click Run now") ||
		strings.Contains(got, "click Repair") {
		t.Errorf("F2: with its entry back, status says the next backup skips it:\n%s", got)
	}
}
