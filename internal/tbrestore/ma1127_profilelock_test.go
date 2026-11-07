package tbrestore

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// covers: MA-1127, R40, S67
// Thunderbird's profile lock as the restore holds it (design §5.2 note items 1 and
// 2): while held, another process — standing for a starting Thunderbird, with
// Mozilla's own lock call — sees it held (F_GETLK on macOS and Linux) and cannot take
// it; the lock file is never written (its bytes are what they were) and no `lock`
// symlink is made; after Release the other process takes it; the lock file is left in
// place, as Thunderbird leaves it.
func TestRestoreProfileLockHeldAndReleased(t *testing.T) {
	noProcesses(t)
	profile, _ := localProfile(t)
	lockPath := filepath.Join(profile, ParentLockName)
	writeFile(t, lockPath, "left by Thunderbird")

	l, err := TakeProfileLock(profile)
	if err != nil {
		t.Fatal(err)
	}
	if got := childSays(t, "getlk", lockPath); got != "held" {
		t.Fatalf("another process sees the lock held: %q", got)
	}
	if got := childSays(t, "take", lockPath); got != "refused" {
		t.Fatalf("another process cannot take it: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(profile, "lock")); !os.IsNotExist(err) {
		t.Fatal("the restore never makes a `lock` symlink")
	}
	l.Release()
	l.Release() // twice is harmless
	if b, err := os.ReadFile(lockPath); err != nil || string(b) != "left by Thunderbird" {
		t.Fatalf("the lock file is never written and is left in place: %q %v", b, err)
	}
	if got := childSays(t, "take", lockPath); got != "took" {
		t.Fatalf("after Release another process takes it: %q", got)
	}

	// Absent, it is created (empty) and still never written.
	profile2, _ := localProfile(t)
	l2, err := TakeProfileLock(profile2)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(filepath.Join(profile2, ParentLockName))
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != 0 {
		t.Fatalf("an absent lock file is created empty: %v %v", fi, err)
	}
	l2.Release()
}

// covers: MA-1127, R40, S67
// The sole-opener rule (lead's call 2026-11-07, from the Mac lab: on macOS closing a
// second handle of .parentlock in the holding process drops the lock): while this
// process holds a profile's lock, the restore's own running check answers from the
// lock's record (SelfHeld) and opens no second handle, so the lock survives it —
// another process still sees it held after several checks; and a second take in this
// process is refused, never a second open.
func TestRestoreProfileLockSurvivesItsOwnProbe(t *testing.T) {
	noProcesses(t)
	profile, _ := localProfile(t)
	lockPath := filepath.Join(profile, ParentLockName)
	l, err := TakeProfileLock(profile)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	for i := 0; i < 3; i++ {
		s := CheckRunning(profile)
		if !s.ByLock || !s.SelfHeld {
			t.Fatalf("the restore's own check sees its own lock: %+v", s)
		}
	}
	if got := childSays(t, "getlk", lockPath); got != "held" {
		t.Fatalf("the lock survives the restore's own probe: another process says %q", got)
	}
	_, err = TakeProfileLock(profile)
	refusedAs(t, err, "W-RUNNING", nil, "Thunderbird is open")
	if got := childSays(t, "getlk", lockPath); got != "held" {
		t.Fatalf("a second take in this process opens no second handle: %q", got)
	}
}

// covers: MA-1127, R40, S67
// Linux: the lock is the per-handle kind (F_OFD_SETLK), so it stays held after this
// process opens and closes another descriptor of .parentlock (the Linux lab's
// finding; the classic kind loses it).
func TestRestoreProfileLockSurvivesAnotherDescriptorOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the per-handle lock kind exists only on Linux; macOS is held by the sole-opener rule (TestRestoreProfileLockSurvivesItsOwnProbe)")
	}
	noProcesses(t)
	profile, _ := localProfile(t)
	lockPath := filepath.Join(profile, ParentLockName)
	l, err := TakeProfileLock(profile)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	f, err := os.Open(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if got := childSays(t, "getlk", lockPath); got != "held" {
		t.Fatalf("an OFD lock survives another descriptor's close: %q", got)
	}
}
