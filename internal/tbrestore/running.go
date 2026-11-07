package tbrestore

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mail-archive-tool/internal/procs"
)

// RunningSignal is why the restore counts Thunderbird as running (design §5.1 as
// folded): any one signal is enough, so a mistake only refuses.
type RunningSignal struct {
	// ByLock is check A: the profile's lock is held — the `lock` symlink names a live
	// pid (or one that cannot be read), Windows' parent.lock is held open, or, on macOS
	// and Linux, someone holds a POSIX lock on .parentlock (the non-taking probe the Mac
	// lab of 2026-11-06 showed is needed: Thunderbird 159 on macOS makes no `lock`
	// symlink, lead's call 2026-11-07).
	ByLock bool
	// ByProcess is check B: a Thunderbird process runs (for this user on macOS and
	// Linux; for any user on Windows, the safe direction), or the process list could
	// not be read.
	ByProcess bool
	// SelfHeld: this process itself holds the profile's Thunderbird lock (a restore of
	// this process is writing into it). Counted under ByLock.
	SelfHeld bool
}

// Running reports whether any signal fired.
func (s RunningSignal) Running() bool { return s.ByLock || s.ByProcess }

// processProbe lists Thunderbird processes: n is how many, known is false when the
// list could not be read (counted as running). The real probes are per platform; a
// test replaces this one (the external binary and the OS list are the seam).
var processProbe = thunderbirdProcessCount

// CheckRunning is checks A and B for a profile, at the plan (Check, -plan) and at
// Start before Thunderbird's lock is taken by the restore. It never opens a second
// handle of .parentlock while this process holds one: a profile this process has
// locked answers from the lock's own record (see ProfileLock).
func CheckRunning(profile string) RunningSignal {
	var s RunningSignal
	if symlinkLockHeld(profile) {
		s.ByLock = true
	}
	held, self := parentLockProbe(profile)
	if held {
		s.ByLock = true
		s.SelfHeld = self
	}
	if n, known := processProbe(); !known || n > 0 {
		s.ByProcess = true
	}
	return s
}

// ProcessRunning is check B alone: what is checked again after the restore takes
// Thunderbird's lock, since A would then see the restore's own lock (§5.1 note).
func ProcessRunning() bool {
	n, known := processProbe()
	return !known || n > 0
}

// symlinkLockHeld is the `lock` symlink of Linux Thunderbird (and the .parentlock
// symlink Mozilla falls back to when fcntl locking fails): held when it names a live
// pid, or a pid that cannot be read (as Mozilla treats it); a dead pid is stale. A pid
// reused by another program still counts, as presync does.
func symlinkLockHeld(profile string) bool {
	for _, name := range []string{"lock", ".parentlock"} {
		p := filepath.Join(profile, name)
		fi, err := os.Lstat(p)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			continue
		}
		target, err := os.Readlink(p)
		if err != nil {
			return true
		}
		pid := lockTargetPID(target)
		if pid <= 0 || procs.Alive(pid) {
			return true
		}
	}
	return false
}

// lockTargetPID is the pid of a Mozilla lock target ("127.0.1.1:+2759488",
// "host:12345", "+2759488"); 0 when it names none.
func lockTargetPID(target string) int {
	s := target
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimPrefix(strings.TrimSpace(s), "+")
	pid, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return pid
}

// isThunderbirdExe is presync's name rule (running.go:44-52): the executable's base
// name, case-blind.
func isThunderbirdExe(exe string) bool {
	switch strings.ToLower(filepath.Base(strings.TrimSuffix(exe, " (deleted)"))) {
	case "thunderbird", "thunderbird-bin", "thunderbird.exe":
		return true
	}
	return false
}
