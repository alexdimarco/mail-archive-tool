package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/state"
)

// covers: MA-908, R37
// Switching the Thunderbird sync off removes the archive's sync streak
// (design-presync-attended §5.3 "Removing the file", slice AS5; C-34), so a later
// warning never counts misses from before. Positive twin first: saves that keep the
// sync on — a cadence change, the sync switched on — keep the streak. Then the
// sync-alone save that switches it off removes it, and so does a save that switches it
// off together with a cadence change.
func TestSwitchingTheSyncOffRemovesTheStreak(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	plant := func() {
		t.Helper()
		s := state.SyncStreak{}.Miss("owner-active-skipped", time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC))
		if err := state.WriteSyncStreak(out, s); err != nil {
			t.Fatal(err)
		}
	}
	present := func() bool {
		_, err := os.Lstat(filepath.Join(out, state.SyncStreakName))
		return !errors.Is(err, os.ErrNotExist)
	}
	if body := g.post(`{"interval":"weekly","time":"03:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create with the sync on: %s", body)
	}
	g.recordLast()
	plant()
	if body := g.post(`{"interval":"daily","time":"03:00"}`); !strings.Contains(body, `"ok":true`) || !present() {
		t.Fatalf("positive twin: a cadence change with the sync kept on: %s, streak kept %v", body, present())
	}
	g.recordLast()

	if body := g.post(`{"interval":"daily","time":"03:00","presync":false}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the sync switched off alone: %s", body)
	}
	if j, err := job.Read(g.jobPath); err != nil || j.ThunderbirdSync != nil {
		t.Fatalf("the job still syncs: %v", err)
	}
	if present() {
		t.Error("switching the sync off (alone) left the sync streak")
	}

	plant()
	if body := g.post(`{"interval":"daily","time":"03:00","presync":true}`); !strings.Contains(body, `"ok":true`) || !present() {
		t.Fatalf("positive twin: the sync switched back on: %s, streak kept %v", body, present())
	}
	g.recordLast()
	if body := g.post(`{"interval":"weekly","time":"04:00","presync":false}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the sync off with a cadence change: %s", body)
	}
	if present() {
		t.Error("switching the sync off with a cadence change left the sync streak")
	}
}
