package tbrestore

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// covers: MA-1127, R40, S67
// ADV-01 (the R3–R5 adversarial pass, settled 2026-11-07): the sole-opener registry is
// keyed by the lock file itself (device and inode on macOS and Linux, volume and file id
// on Windows), not by how the profile's path is spelled. While the restore holds a
// profile's lock, a running check or a second take of the same lock file reached another
// way — another profile folder whose lock file is a hard link to this one's, or, on
// macOS, the same folder spelled through /System/Volumes/Data — answers from the lock's
// own record (SelfHeld) and opens no second handle, so a starting Thunderbird (a child
// process with Mozilla's own lock call) is still refused afterwards. Before the fix, on
// macOS the second handle's close dropped the lock and the child took it.
func TestRestoreProfileLockRegistryIsKeyedByTheFile(t *testing.T) {
	noProcesses(t)
	held := func(t *testing.T, profile, alias string) {
		t.Helper()
		lock := filepath.Join(profile, ParentLockName)
		l, err := TakeProfileLock(profile)
		if err != nil {
			t.Fatal(err)
		}
		defer l.Release()
		if got := childSays(t, "take", lock); got != "refused" {
			t.Fatalf("the lock is held to begin with: %q", got)
		}
		for i := 0; i < 2; i++ {
			s := CheckRunning(alias)
			if !s.ByLock || !s.SelfHeld {
				t.Fatalf("the lock file reached as %s is answered from the lock's own record: %+v", alias, s)
			}
		}
		_, err = TakeProfileLock(alias)
		refusedAs(t, err, "W-RUNNING", nil, "Thunderbird is open")
		if n := keptHandles(l); n != 0 {
			t.Fatalf("the registry answers by the file's identity before any open: %d second handles were opened", n)
		}
		if got := childSays(t, "take", lock); got != "refused" {
			t.Fatalf("a starting Thunderbird is still refused after the checks of %s: %q", alias, got)
		}
		if got := childSays(t, "getlk", lock); got != "held" {
			t.Fatalf("another process still sees the lock held: %q", got)
		}

		// The race's guard on its own (macOS and Linux; a Windows handle's close drops no
		// lock): a probe that opened the held file anyway (the name swapped for it after
		// the identity check) keeps that handle with the lock, never closing it before
		// Release, and answers self.
		if runtime.GOOS == "windows" {
			return
		}
		held.Lock()
		h, self := probeLockFile(alias)
		held.Unlock()
		if !h || !self {
			t.Fatalf("a probe that opened the held file answers held by this process: %v %v", h, self)
		}
		if got := childSays(t, "take", lock); got != "refused" {
			t.Fatalf("a starting Thunderbird is still refused after the probe's open: %q", got)
		}
	}

	t.Run("hard link", func(t *testing.T) {
		profile, _ := localProfile(t)
		writeFile(t, filepath.Join(profile, ParentLockName), "")
		other := filepath.Join(t.TempDir(), "q.default")
		if err := os.MkdirAll(other, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(filepath.Join(profile, ParentLockName), filepath.Join(other, ParentLockName)); err != nil {
			t.Fatalf("a hard link on the temp volume: %v", err)
		}
		held(t, profile, other)
	})

	t.Run("firmlink spelling", func(t *testing.T) {
		if runtime.GOOS != "darwin" {
			t.Skip("the /System/Volumes/Data spelling exists only on macOS; the hard link case covers this platform")
		}
		profile, _ := localProfile(t)
		alias := "/System/Volumes/Data" + profile
		a, err1 := os.Stat(alias)
		b, err2 := os.Stat(profile)
		if err1 != nil || err2 != nil || !os.SameFile(a, b) {
			t.Fatalf("the temp folder has its /System/Volumes/Data spelling on macOS: %v %v", err1, err2)
		}
		held(t, profile, alias)
	})
}
