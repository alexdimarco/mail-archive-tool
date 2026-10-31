package app

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-908, MA-910, R32, S65
// A run names the sync record it starts (the gate-2 adversarial pass after AS5 and AS6),
// through a real app.Run with a scripted sync: positive twin first, the first scheduled
// miss writes a record with a RecordID; later runs keep it, a reset included; a scheduled
// run whose job has no sync removes the record, and the next record gets a new name, as
// does one started over a damaged record. So its streak, again Serial 1, is not the one
// an earlier record's told-file names.
func TestARunNamesTheSyncRecordItStarts(t *testing.T) {
	out, in := tmpDir(t), completeMaildir(t)
	name := func(label string) string {
		t.Helper()
		s, ok := streakOnDisk(t, out)
		if !ok || s.RecordID == "" {
			t.Fatalf("%s: no named record (%+v, file %v)", label, s, ok)
		}
		return s.RecordID
	}
	miss := func() {
		t.Helper()
		syncRun(t, out, in, kindScheduled, true, outcome("owner-active-skipped", false, ""))
	}
	miss()
	first := name("positive twin: the first scheduled miss")
	miss()
	syncRun(t, out, in, kindRunNow, true, outcome("ok", true, "ok"))
	miss()
	if got := name("later runs"); got != first {
		t.Fatalf("later runs renamed the record: %q, was %q", got, first)
	}

	syncRun(t, out, in, kindScheduled, false, nil) // the sync switched off: the record goes
	miss()
	second := name("a record started after the sync was switched back on")
	if second == first {
		t.Fatalf("a record started again kept the old name %q", first)
	}
	if s, _ := streakOnDisk(t, out); s.Serial != 1 {
		t.Fatalf("the restarted record's streak: serial %d, want 1", s.Serial)
	}

	if err := os.WriteFile(filepath.Join(out, state.SyncStreakName), []byte("{damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	miss()
	if third := name("a record started over a damaged one"); third == second || third == first {
		t.Fatalf("a record started over a damaged one is named %q (before: %q, %q)", third, first, second)
	}
}
