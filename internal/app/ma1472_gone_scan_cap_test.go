package app

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/state"
)

// covers: MA-1472, R37
// A run reads at most state.MaxGoneWarnings of the previous record's gone-mailbox warnings when it
// carries first-found dates (the 1.7.5 adversarial pass's INS-06): a planted record of thousands
// makes at most that many looks. Positive twin first: a warning within the cap carries its date.
func TestARunReadsAtMostSoManyGoneWarnings(t *testing.T) {
	home := tmpDir(t)
	// Amended 2026-11-21 (RV2-06): the planted paths lie where auto-discovery looks, as a run's own
	// warnings do; a warning outside the places is now passed over before any look. Assertions kept.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	parent := filepath.Join(home, ".thunderbird", "p", "ImapMail")
	b := filepath.Join(parent, "imap.qzb.example")
	first := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	ws := []state.RunWarning{{Code: state.MailboxGoneCode, Message: "x", Path: b, Since: first}}
	for i := 0; i < 10_000; i++ {
		ws = append(ws, state.RunWarning{Code: state.MailboxGoneCode, Message: "x", Path: filepath.Join(parent, fmt.Sprintf("imap.qz%d.example", i)), Since: first})
	}
	out := tmpDir(t)
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: first, Finished: first, Warnings: ws}); err != nil {
		t.Fatal(err)
	}
	var looks atomic.Int64
	t.Cleanup(autoplace.WatchLooksForTest(func(string) { looks.Add(1) }))
	s := []SkippedInput{{Path: b}}
	resolveGoneSince(out, s, first.Add(time.Hour))
	if !s[0].Since.Equal(first) {
		t.Fatalf("positive twin: the warning within the cap carries its date: %s", s[0].Since)
	}
	// The planted ones after b: b matches at once, so ask for one no warning names.
	looks.Store(0)
	c := []SkippedInput{{Path: filepath.Join(parent, "imap.qzc.example")}}
	resolveGoneSince(out, c, first.Add(time.Hour))
	if n := looks.Load(); n > int64(state.MaxGoneWarnings)+1 {
		t.Errorf("a planted record of 10001 gone mailboxes made %d looks, want at most %d", n, state.MaxGoneWarnings+1)
	}
}
