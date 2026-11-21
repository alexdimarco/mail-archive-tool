package health

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

// looked records every path autoplace looks at on disk (autoplace.WatchLooksForTest), for the
// length of the test.
type looked struct {
	mu    sync.Mutex
	paths []string
}

func watchLooks(t *testing.T) *looked {
	t.Helper()
	l := &looked{}
	t.Cleanup(autoplace.WatchLooksForTest(func(p string) { l.mu.Lock(); l.paths = append(l.paths, p); l.mu.Unlock() }))
	return l
}

// at is the first path looked at that holds any of parts, or "".
func (l *looked) at(parts ...string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, p := range l.paths {
		for _, s := range parts {
			if strings.Contains(p, s) {
				return p
			}
		}
	}
	return ""
}

// covers: MA-869, MA-1479, R37
// A network (UNC) or non-local path planted in a stopped archive's record is never looked at by
// status (C-17; the 1.7.5 adversarial pass's OUT-01 and INS-01): health.GoneMailboxRepairable says
// no before any file check when the record's job lists one, as the refused input or as another, so
// status keeps today's words. The planted name is also made to exist as a relative folder (on
// macOS and Linux it is one), so an answer that depended on a look would show. Positive twin first:
// a local listed input that is there is looked at, and the helper says yes (MA-1479).
func TestStatusNeverLooksAtANetworkPathPlantedInTheRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	profile := filepath.Join(home, ".thunderbird", "abcd1234.default-release")
	a, b := filepath.Join(profile, "ImapMail", "imap.qza.example"), filepath.Join(profile, "ImapMail", "imap.qzb.example")
	if err := os.MkdirAll(a, 0o755); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	plant := func(t *testing.T, refused string, inputs ...string) string {
		t.Helper()
		out := t.TempDir()
		if err := state.WriteSourceStreak(out, state.SourceStreak{ConsecutiveFails: 2, FirstUnreachable: t0, LastUnreachable: t0.Add(state.SurfaceAfter), LastKind: "local-source-unreachable"}); err != nil {
			t.Fatal(err)
		}
		job := []string{"-out", out, "-mode", "incremental", "-auto"}
		for _, in := range inputs {
			job = append(job, "-input", in)
		}
		at := t0.Add(state.SurfaceAfter)
		if err := state.WriteLastRun(out, state.LastRun{Status: state.RunFailed, Started: at, Finished: at, Error: autoplace.GoneText(refused), Job: job}); err != nil {
			t.Fatal(err)
		}
		return out
	}

	l := watchLooks(t)
	out := plant(t, b, a, b)
	if !GoneMailboxRepairable(out) || l.at(a) == "" {
		t.Fatalf("positive twin: a local input that is there is looked at and the helper says yes (looked at %v)", l.paths)
	}
	if got := strings.Join(Assess(Gather(out, ""), time.Now()).Reasons, "\n"); !strings.Contains(got, "if a mail account was removed") {
		t.Fatalf("positive twin: status says Repair:\n%s", got)
	}

	t.Chdir(t.TempDir())
	for _, host := range []string{`\\evil-host\share\Mail`, "//evil-host/share/Mail"} {
		madeHere := os.MkdirAll(host, 0o755) == nil // a relative folder on macOS and Linux
		for name, c := range map[string]struct {
			refused string
			inputs  []string
		}{
			"as the refused input": {host, []string{a, host}},
			"as another input":     {b, []string{b, host}},
			"beside a local input": {b, []string{a, b, host}},
		} {
			l := watchLooks(t)
			out := plant(t, c.refused, c.inputs...)
			if GoneMailboxRepairable(out) {
				t.Errorf("%s %s: the helper says yes (made here: %v)", host, name, madeHere)
			}
			got := strings.Join(Assess(Gather(out, ""), time.Now()).Reasons, "\n")
			if strings.Contains(got, "if a mail account was removed") || !strings.Contains(got, "reconnect the source (its mailbox or mail folder) to resume backups") {
				t.Errorf("%s %s: status must keep today's words:\n%s", host, name, got)
			}
			if p := l.at("evil-host"); p != "" {
				t.Errorf("%s %s: the planted path was looked at: %s", host, name, p)
			}
		}
	}
}
