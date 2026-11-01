package archdelete

import (
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/lockfile"
)

// covers: MA-968, R36
func TestALockARunTakesAfterStep7sReleaseIsNeverRemoved(t *testing.T) {
	// del-s1b-A2-06, A3-09: a run waiting for the lock takes it in the instant after
	// step 7 releases it. Its lock file is never removed under it (on macOS and Linux
	// removing a locked file succeeds): the file stays, the delete says the lock file
	// could not be removed, and the run still holds its own lock.
	dir := smallArchive(t, t.TempDir())
	// Positive twin first: with no run, the lock file goes and the folder with it.
	if res := newRig(t, smallArchive(t, t.TempDir())).run(t, false); res.Status != StatusComplete || res.LockFileLeft || !res.FolderGone {
		t.Fatalf("%s", res.Message())
	}
	r := newRig(t, dir)
	var run *lockfile.Lock
	old := afterRelease
	t.Cleanup(func() {
		afterRelease = old
		run.Release()
	})
	afterRelease = func(folder string) {
		l, err := lockfile.AcquireAs(filepath.Join(folder, lockName), "export")
		if err != nil {
			t.Errorf("the run could not take the released lock: %v", err)
			return
		}
		run = l
	}
	res := r.run(t, false)
	if run == nil {
		t.FailNow()
	}
	if res.Status != StatusComplete || !res.LockFileLeft || !hasLine(res, msgLockFileLeft) || !exists(filepath.Join(dir, lockName)) {
		t.Fatalf("%s", res.Message())
	}
	if err := run.StillHeld(); err != nil {
		t.Fatalf("the run's lock file was removed under it: %v", err)
	}
}

// covers: MA-968, R36
func TestStep8IsSkippedWhileARunReallyHoldsTheLock(t *testing.T) {
	// del-s1b-A2-09: step 8 runs only when the folder holds an UNHELD lock file and
	// litter (encmark's Gone). Here a real run holds the lock when step 8 looks: the
	// key is not forgotten, the row is kept, and the run keeps its lock file.
	dir := encryptedClean(t)
	r := newRig(t, dir)
	r.x.Deps.Record = recordedAt(dir)
	faultOn(t, nil, func(rel string) error {
		if rel == lockName {
			return inUseErr(rel) // step 7 leaves the lock file, so the folder is kept
		}
		return nil
	})
	var run *lockfile.Lock
	t.Cleanup(func() { run.Release() })
	r.x.BeforeStep = func(step int) error {
		if step == 8 {
			l, err := lockfile.AcquireAs(filepath.Join(dir, lockName), "export")
			if err != nil {
				t.Errorf("the run could not take the lock: %v", err)
				return nil
			}
			run = l
		}
		return nil
	}
	res := r.run(t, false)
	if run == nil {
		t.FailNow()
	}
	if !res.KeySkipped || !hasLine(res, msgKeySkipped) || len(r.kr.asked) != 0 || len(r.kr.forgot) != 0 || len(r.rows) != 0 {
		t.Fatalf("%s (asked %q forgot %v rows %q)", res.Message(), r.kr.asked, r.kr.forgot, r.rows)
	}
	if err := run.StillHeld(); err != nil {
		t.Fatalf("the run keeps its lock file: %v", err)
	}

	// Positive twin: the same kept folder with the lock file unheld: step 8 runs.
	d2 := encryptedClean(t)
	r2 := newRig(t, d2)
	r2.x.Deps.Record = recordedAt(d2)
	r2.kr.folder = ""
	if res := r2.run(t, false); res.KeySkipped || len(r2.kr.forgot) != 1 {
		t.Fatalf("%s", res.Message())
	}
}
