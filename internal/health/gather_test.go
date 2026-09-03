package health

import (
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// covers: MA-75, MA-76, R18, S29
// Liveness comes from the archive lock, not from a pid number: a "running"
// record while the lock is held is a run in progress; the same record with the
// lock free is a run that never finished — even if the recorded pid happens to
// be alive again (pid reuse after a reboot).
func TestGatherUsesTheLockForLiveness(t *testing.T) {
	out := t.TempDir()
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunRunning, Started: time.Now().Add(-time.Minute), PID: 1}); err != nil {
		t.Fatal(err)
	}
	m, _ := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	m.Add("k", state.Record{Path: "x.html", Folder: "F"})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}

	in := Gather(out, "")
	in.PIDAlive = func(int) bool { return true } // pid 1 is always alive: must not matter
	if in.LockHeld {
		t.Fatal("lock reported held while free")
	}
	if rep := Assess(in, time.Now()); rep.Posture != "RED" {
		t.Errorf("free lock + running record: posture %s, want RED (%v)", rep.Posture, rep.Reasons)
	}

	held, err := lockfile.Acquire(filepath.Join(out, lockfile.Name))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	in = Gather(out, "")
	in.PIDAlive = func(int) bool { return false }
	if !in.LockHeld {
		t.Fatal("lock reported free while held")
	}
	if rep := Assess(in, time.Now()); rep.Posture == "RED" {
		t.Errorf("held lock + running record: posture RED (%v)", rep.Reasons)
	}
}
