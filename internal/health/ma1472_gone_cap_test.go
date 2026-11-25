package health

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1472, R37
// Status says at most state.MaxGoneWarnings gone mailboxes (the 1.7.5 adversarial pass's INS-06): a
// planted record with thousands of mailbox_gone warnings shows that many, then one line saying how
// many more are not listed. Positive twin first: three gone mailboxes are three lines, and no such
// line.
func TestStatusSaysAtMostSoManyGoneMailboxes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	started := time.Date(2026, 11, 20, 2, 0, 0, 0, time.UTC)
	report := func(n int) (string, Report) {
		var ws []state.RunWarning
		for i := 0; i < n; i++ {
			ws = append(ws, state.RunWarning{Code: state.MailboxGoneCode, Message: "x", Since: started,
				Path: filepath.Join(home, ".thunderbird", "p", "ImapMail", fmt.Sprintf("imap.qz%d.example", i))})
		}
		rep := Assess(Input{Out: t.TempDir(), LastRunState: state.LastRunPresent, LastRun: state.LastRun{Status: state.RunOK, Started: started, Finished: started, Warnings: ws}}, started)
		return strings.Join(rep.Reasons, "\n"), rep
	}
	count := func(s string) int { return strings.Count(s, "WARN: a mailbox this archive backs up is no longer on this computer") }
	if got, _ := report(3); count(got) != 3 || strings.Contains(got, "not listed here") {
		t.Fatalf("positive twin: three gone mailboxes are three lines:\n%s", got)
	}
	// RF-08 (the 1.7.5 re-verify): one more is said in the singular.
	if got, _ := report(state.MaxGoneWarnings + 1); !strings.Contains(got, "1 more mailbox this archive backs up is no longer on this computer (not listed here)") {
		t.Errorf("one past the cap is said in the singular:\n%s", got[len(got)-300:])
	}
	got, _ := report(10_000)
	if n := count(got); n != state.MaxGoneWarnings {
		t.Errorf("a planted record of 10000 gone mailboxes shows %d lines, want %d", n, state.MaxGoneWarnings)
	}
	if !strings.Contains(got, fmt.Sprintf("%d more mailboxes this archive backs up are no longer on this computer (not listed here)", 10_000-state.MaxGoneWarnings)) {
		t.Errorf("the lines past the cap are said as one count:\n%s", got[len(got)-400:])
	}
}
