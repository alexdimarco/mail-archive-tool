package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// covers: MA-859, R37
// The bounded lock skip (design-desktop-settings-card 3.7, R3C-12): a scheduled run refused
// because another run holds the archive's lock skips quietly only while that holder started less
// than 24 hours ago, and its line names the real holder from the lock's verb. Past 24 hours, with
// a start that can't be read, or for any other error, it is not quiet: the run counts as a
// failure. Positive twin first: a holder started an hour ago is quiet.
func TestALockHeldUnderADayIsAQuietSkipNamingItsHolder(t *testing.T) {
	out := t.TempDir()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	held := fmt.Errorf("archive is in use: %w", lockfile.ErrHeld)
	plant := func(started time.Time, verb string) {
		line := "pid=1 started=" + started.Format(time.RFC3339) + " host=h nonce=n"
		if verb != "" {
			line += " verb=" + verb
		}
		if err := os.WriteFile(filepath.Join(out, lockfile.Name), []byte(line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for verb, words := range map[string]string{
		"export": "an earlier backup of this archive", "graph": "a Microsoft 365 capture into this archive",
		"verify": "a check of this archive (verify)", "reindex": "a rebuild of this archive's search index",
		"rebuild": "a rebuild of this archive's search index", "": "another MailArchive task",
	} {
		plant(now.Add(-time.Hour), verb)
		line, quiet := ScheduledLockSkip(out, held, now)
		if !quiet || !strings.Contains(line, "skipped: "+words+" started at") {
			t.Errorf("verb %q: quiet=%v line %q", verb, quiet, line)
		}
	}
	plant(now.Add(-QuietSkipWindow), "export")
	if _, quiet := ScheduledLockSkip(out, held, now); quiet {
		t.Error("a holder started 24 hours ago must count as a failure, not a quiet skip")
	}
	plant(now.Add(48*time.Hour), "export")
	if _, quiet := ScheduledLockSkip(out, held, now); quiet {
		t.Error("a holder whose start is two days ahead is not proven recent")
	}
	if err := os.WriteFile(filepath.Join(out, lockfile.Name), []byte("pid=1 host=h\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, quiet := ScheduledLockSkip(out, held, now); quiet {
		t.Error("a holder whose start can't be read is not proven recent")
	}
	plant(now.Add(-time.Hour), "export")
	if _, quiet := ScheduledLockSkip(out, errors.New("disk full"), now); quiet {
		t.Error("an error that is not a held lock was skipped quietly")
	}
}

// covers: MA-871, R37
// An inconclusive run after an old streak moves nothing, so it does not auto-pause: the streak
// stays as the last unreachable run left it, measured between runs, never to now. And a
// scheduled run refused because its local source is gone moves the streak (the refusal path
// records it), which is what lets the local job warn and auto-pause at all.
func TestAnInconclusiveRunAfterAnOldStreakDoesNotAutoPause(t *testing.T) {
	out := t.TempDir()
	t0 := time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC)
	old := state.SourceStreak{ConsecutiveFails: 2, FirstUnreachable: t0, LastUnreachable: t0.Add(24 * time.Hour)}
	if err := state.WriteSourceStreak(out, old); err != nil {
		t.Fatal(err)
	}
	updateSourceStreak(out, errors.New("search index busy"), t0.Add(60*24*time.Hour), false, nil)
	s, err := state.ReadSourceStreak(out)
	if err != nil || !s.LastUnreachable.Equal(old.LastUnreachable) || s.ConsecutiveFails != 2 || s.AutoPauseDue() || s.SurfaceLoudly() {
		t.Fatalf("an inconclusive run 60 days on moved the streak or tripped it: %+v %v", s, err)
	}

	// A refused scheduled run whose source is gone extends it (positive twin: it does move).
	m := filepath.Join(out, ".mailarchive-manifest.json")
	if err := os.WriteFile(m, []byte(`{"version":6,"stores":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(t.TempDir(), "gone")
	_, rerr := DiscoverInputs([]string{gone}, false)
	RecordRefusedRun(Options{Out: out, Unattended: true}, withKind(state.FailSourceUnreadable, rerr))
	if s, _ := state.ReadSourceStreak(out); s.ConsecutiveFails != 3 || !s.LastUnreachable.After(old.LastUnreachable) {
		t.Fatalf("a refused run with its source gone did not extend the streak: %+v", s)
	}
}
