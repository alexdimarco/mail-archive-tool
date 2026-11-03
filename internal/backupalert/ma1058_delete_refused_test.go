package backupalert

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/uninstall"
)

// covers: MA-1058, R36
// The count of scheduled runs refused for a delete this computer never began
// (docs/design-archive-delete.md §15.10 slice 6) is kept beside the Desktop settings, never in the
// archive folder and never named .json: each refused run extends it as an unreachable run extends
// the source-loss streak, and it is due once the refused runs span 30 days (the same rule, at least
// two runs); any other run ends it and removes the file. ClearStreak (a schedule's create and
// Remove, and an auto-pause) removes it, and uninstall removes it. Positive twin first: a refused
// run writes the count.
func TestMA1058_TheRefusedRunCountIsKeptBesideTheSettings(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "desktop-settings.json")
	out := filepath.Join(t.TempDir(), "archive")
	key := KeyFor(out)
	p := DeleteRefusedPath(settings, key)
	if filepath.Dir(p) != filepath.Dir(settings) || strings.HasSuffix(p, ".json") || !strings.HasSuffix(p, DeleteRefusedSuffix) {
		t.Fatalf("the count is at %s", p)
	}
	t0 := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)

	// Positive twin: a refused run writes the count.
	s, err := NoteDeleteRefused(settings, key, true, t0)
	assure.Reached(t, err == nil && s.ConsecutiveFails == 1 && !s.AutoPauseDue(), "the first refused run counts once")
	_, serr := os.Lstat(p)
	assure.Reached(t, serr == nil, "the count's file beside the settings")

	s, err = NoteDeleteRefused(settings, key, true, t0.Add(state.AutoPauseAfter-time.Hour))
	assure.Reached(t, err == nil && s.ConsecutiveFails == 2 && !s.AutoPauseDue(), "29 days and 23 hours: not due")
	s, err = NoteDeleteRefused(settings, key, true, t0.Add(state.AutoPauseAfter))
	assure.Reached(t, err == nil && s.ConsecutiveFails == 3 && s.AutoPauseDue(), "30 days of refused runs: due")
	if _, err := os.Lstat(filepath.Join(out, state.SourceStreakName)); !os.IsNotExist(err) {
		t.Error("the count wrote the archive folder's source-loss record")
	}

	// Any other run ends the count.
	s, err = NoteDeleteRefused(settings, key, false, t0.Add(state.AutoPauseAfter+time.Hour))
	_, serr = os.Lstat(p)
	assure.Reached(t, err == nil && s.ConsecutiveFails == 0 && os.IsNotExist(serr), "a run that was not refused ends the count")
	s, _ = NoteDeleteRefused(settings, key, true, t0.Add(2*state.AutoPauseAfter))
	assure.Reached(t, s.ConsecutiveFails == 1 && !s.AutoPauseDue(), "a new count after the end starts at one")

	// ClearStreak removes it.
	if err := ClearStreak(settings, key); err != nil {
		t.Fatal(err)
	}
	_, serr = os.Lstat(p)
	assure.Reached(t, os.IsNotExist(serr), "ClearStreak removes the count")

	// A file that cannot be read is an error and is left alone; a damaged one starts again.
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NoteDeleteRefused(settings, key, true, t0); err == nil {
		t.Error("a count that cannot be read was not an error")
	}
	if fi, err := os.Lstat(p); err != nil || !fi.IsDir() {
		t.Error("a count that cannot be read was replaced")
	}
	os.Remove(p)
	big := strings.Repeat(" ", maxDeleteRefusedBytes+1)
	if err := os.WriteFile(p, []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NoteDeleteRefused(settings, key, true, t0); err == nil {
		t.Error("a count too large to read was not an error")
	}
	if b, err := os.ReadFile(p); err != nil || string(b) != big {
		t.Error("a count too large to read was replaced")
	}
	if err := os.WriteFile(p, []byte("{damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err = NoteDeleteRefused(settings, key, true, t0)
	assure.Reached(t, err == nil && s.ConsecutiveFails == 1, "a damaged count starts again at one")

	// Uninstall removes it.
	targets, err := uninstall.ConfigTargets(filepath.Dir(settings), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tg := range targets {
		found = found || tg == p
	}
	if !found {
		t.Errorf("uninstall does not remove the count %s (targets %v)", p, targets)
	}
}
