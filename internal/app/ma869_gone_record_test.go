package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/state"
)

// covers: MA-869, MA-1472, R37
// A run that skips a gone mailbox never looks at a network (UNC) or non-local path planted in the
// previous record's mailbox_gone warnings (C-17; the 1.7.5 adversarial pass's INS-01, part of
// OUT-01): resolveGoneSince passes such a warning over before autoplace.GoneSourceID, so its
// first-found date is never carried from it. Positive twin first: the local warning for the same
// input is looked at and carries its date.
func TestARunNeverLooksAtANetworkPathPlantedInTheGoneWarnings(t *testing.T) {
	home := tmpDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	parent := filepath.Join(home, ".thunderbird", "abcd1234.default-release", "ImapMail")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(parent, "imap.qzb.example") // gone
	first := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	start := first.Add(5 * 24 * time.Hour)

	var mu sync.Mutex
	var looked []string
	t.Cleanup(autoplace.WatchLooksForTest(func(p string) { mu.Lock(); looked = append(looked, p); mu.Unlock() }))
	lookedAt := func(part string) string {
		mu.Lock()
		defer mu.Unlock()
		for _, p := range looked {
			if strings.Contains(p, part) {
				return p
			}
		}
		return ""
	}

	out := tmpDir(t)
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: first, Finished: first,
		Warnings: []state.RunWarning{{Code: state.MailboxGoneCode, Message: "x", Path: b, Since: first}}}); err != nil {
		t.Fatal(err)
	}
	s := []SkippedInput{{Path: b}}
	resolveGoneSince(out, s, start)
	if !s[0].Since.Equal(first) || lookedAt(parent) == "" {
		t.Fatalf("positive twin: the local warning carries its date (got %s; looked at %v)", s[0].Since, looked)
	}

	// Amended 2026-11-21 by the re-verify, round 2 (RV2-06): also a local path outside the places
	// auto-discovery looks, passed over before any look, as the Repair helper does since RV-01.
	for _, host := range []string{`\\evil-host\share\Mail`, "//evil-host/share/Mail", "evil-host/share/Mail", filepath.Join(home, "qz-evil-host-outside", "Mail")} {
		mu.Lock()
		looked = nil // each spelling's looks alone
		mu.Unlock()
		out := tmpDir(t)
		if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: first, Finished: first,
			Warnings: []state.RunWarning{{Code: state.MailboxGoneCode, Message: "x", Path: host, Since: first}}}); err != nil {
			t.Fatal(err)
		}
		s := []SkippedInput{{Path: b}}
		resolveGoneSince(out, s, start)
		if p := lookedAt("evil-host"); p != "" {
			t.Errorf("%s: the planted path was looked at: %s", host, p)
		}
		if !s[0].Since.Equal(start) {
			t.Errorf("%s: a planted warning's date was carried: %s", host, s[0].Since)
		}
	}
}
