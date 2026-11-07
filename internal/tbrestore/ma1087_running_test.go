package tbrestore

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// covers: MA-1087, R40, S66, S67
// Check A, the profile's lock (design §5.1 as folded): a `lock` symlink naming a live
// pid (a reused pid too: any live pid counts), or naming no readable pid, is running;
// one naming a dead pid is stale; another process holding the lock file — the POSIX
// lock on .parentlock on macOS and Linux (the non-taking probe, lead's call 2026-11-07:
// Mac Thunderbird 159 makes no `lock` symlink), parent.lock on Windows — is running;
// a free one is not. The probe creates nothing in the profile.
func TestRestoreRunningCheckA(t *testing.T) {
	noProcesses(t)
	if runtime.GOOS != "windows" {
		cases := map[string]struct {
			target string
			want   bool
		}{
			"live pid (this test)":   {"127.0.0.1:+" + strconv.Itoa(os.Getpid()), true},
			"unparseable target":     {"garbage", true},
			"dead pid (stale)":       {"host:+" + strconv.Itoa(deadPID(t)), false},
			"bare plus pid, live":    {"+" + strconv.Itoa(os.Getpid()), true},
			"reused pid, not TB yet": {"host:" + strconv.Itoa(os.Getppid()), true},
		}
		for name, c := range cases {
			t.Run(name, func(t *testing.T) {
				profile, _ := localProfile(t)
				if err := os.Symlink(c.target, filepath.Join(profile, "lock")); err != nil {
					t.Fatal(err)
				}
				before := snapshot(t, profile)
				s := CheckRunning(profile)
				if s.ByLock != c.want || s.ByProcess {
					t.Fatalf("got %+v, want ByLock %v", s, c.want)
				}
				if snapshot(t, profile) != before {
					t.Fatal("the check wrote into the profile")
				}
			})
		}
	}

	t.Run("lock file held by another process", func(t *testing.T) {
		profile, _ := localProfile(t)
		if s := CheckRunning(profile); s.Running() {
			t.Fatalf("no lock file: not running (positive twin): %+v", s)
		}
		lockPath := filepath.Join(profile, ParentLockName)
		kill := childHolds(t, lockPath)
		// The held lock file itself cannot be read on Windows (no sharing): left out of the
		// listing, which still proves nothing else in the profile was made or changed.
		before := snapshot(t, profile, ParentLockName)
		s := CheckRunning(profile)
		if !s.ByLock || s.SelfHeld {
			t.Fatalf("a held lock file is running (not by this process): %+v", s)
		}
		if snapshot(t, profile, ParentLockName) != before {
			t.Fatal("the probe wrote into the profile")
		}
		kill()
		if s := CheckRunning(profile); s.Running() {
			t.Fatalf("a lock file whose holder was killed is free: %+v", s)
		}
	})
}

// covers: MA-1087, R40, S66
// Check B, the process list: any Thunderbird process is running, and so is a list that
// cannot be read (a mistake only refuses). The Linux /proc probe (new code): a process
// of this user whose exe is a Thunderbird (presync's names, an upgraded "(deleted)"
// binary too), or whose exe cannot be read but whose comm is one, counts; another
// user's process, another program and a non-pid entry do not; a /proc that cannot be
// listed is not known.
func TestRestoreRunningCheckB(t *testing.T) {
	profile, _ := localProfile(t)
	for name, c := range map[string]struct {
		n     int
		known bool
		want  bool
	}{"none": {0, true, false}, "one": {1, true, true}, "unknown": {0, false, true}} {
		t.Run(name, func(t *testing.T) {
			was := processProbe
			processProbe = func() (int, bool) { return c.n, c.known }
			defer func() { processProbe = was }()
			if s := CheckRunning(profile); s.ByProcess != c.want || ProcessRunning() != c.want {
				t.Fatalf("got %+v", s)
			}
		})
	}

	t.Run("linux /proc probe", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("the fake /proc tree uses symlinks, which need a privilege on Windows; the probe runs only on Linux")
		}
		root := t.TempDir()
		link := func(pid, target string) {
			if err := os.MkdirAll(filepath.Join(root, pid), 0o700); err != nil {
				t.Fatal(err)
			}
			if target != "" {
				if err := os.Symlink(target, filepath.Join(root, pid, "exe")); err != nil {
					t.Fatal(err)
				}
			}
		}
		link("100", "/usr/lib/thunderbird/thunderbird")
		link("101", "/usr/bin/bash")
		link("102", "")
		writeFile(t, filepath.Join(root, "102", "comm"), "thunderbird-bin\n")
		link("103", "/app/lib/thunderbird/thunderbird (deleted)")
		link("104", "/usr/lib/thunderbird/thunderbird") // another user's
		link("105", "")
		writeFile(t, filepath.Join(root, "105", "comm"), "bash\n")
		link("self", "/usr/lib/thunderbird/thunderbird")
		owner := func(fi os.FileInfo) (int, bool) {
			if fi.Name() == "104" {
				return 4242, true
			}
			return 1000, true
		}
		if n, known := countProcThunderbird(root, 1000, owner); !known || n != 3 {
			t.Fatalf("want 3 Thunderbird processes of this user, got %d %v", n, known)
		}
		if _, known := countProcThunderbird(filepath.Join(root, "missing"), 1000, owner); known {
			t.Fatal("a /proc that cannot be listed is not known")
		}
	})
}

// covers: MA-1087, R40, S66, S67
// At Start the checks run before Thunderbird's lock is taken, and only B after it
// (design §5.1 note): a signal before the take refuses with W-RUNNING and makes no
// lock file; a Thunderbird that appears between the checks and the take's end
// (B after the take) refuses too and releases the lock at once; a free profile is
// taken (positive twin first).
func TestRestoreTakeProfileLockOrder(t *testing.T) {
	noProcesses(t)
	profile, _ := localProfile(t)
	lockPath := filepath.Join(profile, ParentLockName)

	l, err := TakeProfileLock(profile)
	if err != nil {
		t.Fatalf("a closed profile is taken: %v", err)
	}
	l.Release()

	t.Run("B before the take", func(t *testing.T) {
		profile, _ := localProfile(t)
		was := processProbe
		processProbe = func() (int, bool) { return 1, true }
		defer func() { processProbe = was }()
		before := snapshot(t, profile)
		_, err := TakeProfileLock(profile)
		refusedAs(t, err, "W-RUNNING", func() bool { return snapshot(t, profile) == before }, "Thunderbird is open", "nothing was restored", "Check again")
	})
	t.Run("B after the take", func(t *testing.T) {
		calls := 0
		was := processProbe
		processProbe = func() (int, bool) {
			calls++
			if calls >= 3 { // CheckRunning, the check before the take, then the one after
				return 1, true
			}
			return 0, true
		}
		defer func() { processProbe = was }()
		_, err := TakeProfileLock(profile)
		refusedAs(t, err, "W-RUNNING", nil, "Thunderbird is open")
		if calls != 3 {
			t.Fatalf("B is asked before the open, before the take and once after: %d", calls)
		}
		processProbe = func() (int, bool) { return 0, true }
		if got := childSays(t, "take", lockPath); got != "took" {
			t.Fatalf("a refused take releases the lock at once: the child says %q", got)
		}
	})
	t.Run("lock held by another process", func(t *testing.T) {
		kill := childHolds(t, lockPath)
		_, err := TakeProfileLock(profile)
		refusedAs(t, err, "W-RUNNING", nil, "Thunderbird is open")
		kill()
		l, err := TakeProfileLock(profile)
		if err != nil {
			t.Fatalf("after the holder is killed the lock is free (nothing stale): %v", err)
		}
		l.Release()
	})
}

// deadPID is the pid of a child process that has exited.
func deadPID(t *testing.T) int {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^$")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	return c.Process.Pid
}
