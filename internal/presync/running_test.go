package presync

import "testing"

// covers: MA-553
// Running-detection (design 4.4.1), the start gate: Thunderbird counts as running
// on signal A (the profile lock names a live pid) or signal B (a Thunderbird-named
// process); a stale lock whose live pid is NOT Thunderbird is still running and
// flagged; a lock naming a dead pid is stale (not a running signal); a lock with
// no parseable pid is held; with neither signal the gate may proceed to start.
func TestPresyncRunningDetection(t *testing.T) {
	const profile = "/p"

	t.Run("no lock, no process -> not running", func(t *testing.T) {
		if got := detectRunning(newFakeEnv(), profile); got.running {
			t.Errorf("want not running, got %+v", got)
		}
	})
	t.Run("signal A: lock names a live Thunderbird pid", func(t *testing.T) {
		e := newFakeEnv()
		e.lockOn[profile], e.lockP[profile], e.alive[42] = true, 42, true
		e.exeOf[42] = "/Applications/Thunderbird.app/Contents/MacOS/thunderbird"
		if got := detectRunning(e, profile); !got.running || !got.byLock || got.lockNotTB {
			t.Errorf("want running by lock (Thunderbird): %+v", got)
		}
	})
	t.Run("signal B: a Thunderbird process", func(t *testing.T) {
		e := newFakeEnv()
		e.procs = []proc{{pid: 7, exe: "/x/thunderbird"}}
		if got := detectRunning(e, profile); !got.running || !got.byProcess {
			t.Errorf("want running by process: %+v", got)
		}
	})
	t.Run("both signals", func(t *testing.T) {
		e := newFakeEnv()
		e.lockOn[profile], e.lockP[profile], e.alive[42] = true, 42, true
		e.exeOf[42] = "/x/thunderbird"
		e.procs = []proc{{pid: 42, exe: "/x/thunderbird"}}
		if got := detectRunning(e, profile); !got.running || !got.byLock || !got.byProcess {
			t.Errorf("want running by both: %+v", got)
		}
	})
	t.Run("stale lock, dead pid, no process -> not running", func(t *testing.T) {
		e := newFakeEnv()
		e.lockOn[profile], e.lockP[profile], e.alive[99] = true, 99, false
		if got := detectRunning(e, profile); got.running {
			t.Errorf("a dead lock pid is stale, not running: %+v", got)
		}
	})
	t.Run("stale lock reused by a non-Thunderbird live pid -> running, flagged", func(t *testing.T) {
		e := newFakeEnv()
		e.lockOn[profile], e.lockP[profile], e.alive[1234] = true, 1234, true
		e.exeOf[1234] = "/usr/bin/python3"
		if got := detectRunning(e, profile); !got.running || !got.byLock || !got.lockNotTB {
			t.Errorf("want running, flagged not-Thunderbird: %+v", got)
		}
	})
	t.Run("lock with no parseable pid -> running", func(t *testing.T) {
		e := newFakeEnv()
		e.lockOn[profile], e.lockP[profile] = true, 0
		if got := detectRunning(e, profile); !got.running || !got.byLock {
			t.Errorf("an unparseable lock is held: %+v", got)
		}
	})
}
