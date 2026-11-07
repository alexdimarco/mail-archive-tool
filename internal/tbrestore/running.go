package tbrestore

import (
	"net"
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
	// LockErr: ByLock came only from a lock file that could not be opened or asked (no
	// hold was seen), and why; nil otherwise. Still counted as running; RunningRefusal
	// words it by its cause (settled 2026-11-07 [MA-1095 lab BUG-2]).
	LockErr error
	// StaleLink: on macOS and Linux, `.parentlock` is a link naming a pid that is gone —
	// Mozilla's fallback lock left behind by a Thunderbird that did not close normally.
	// Settled 2026-11-07 [ADV-L1]: the plan's check and Start judge it the same way and
	// refuse it in words that say so (W-LOCKFAIL, wLockStale), never "closed" at Check and
	// "Thunderbird is open" at Start. Nothing follows the link.
	StaleLink bool
	// RemoteLink: a lock link (`lock` or `.parentlock`) names another computer's address,
	// so it is held whatever its pid (counted under ByLock). Settled 2026-11-07 [PCT-2]:
	// Mozilla's IsSymlinkStaleLock gives up on such a lock ("Remote lock: give up even if
	// stuck"); the restore refuses it in words that say Thunderbird may be open on that
	// computer (wLockRemote), never "quit it once" and never judged by a pid here.
	RemoteLink bool
}

// Running reports whether any signal fired (a stale lock link included: it refuses).
func (s RunningSignal) Running() bool { return s.ByLock || s.ByProcess || s.StaleLink }

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
	sym, remote := symlinkLockHeld(profile)
	if sym {
		s.ByLock = true
		s.RemoteLink = remote
	}
	held, self, err := parentLockProbe(profile)
	if held {
		s.ByLock = true
		s.SelfHeld = self
		if err != nil && !sym {
			s.LockErr = err
		}
	}
	if n, known := processProbe(); !known || n > 0 {
		s.ByProcess = true
	}
	s.StaleLink = !sym && staleParentLink(profile)
	return s
}

// staleParentLink reports a `.parentlock` link (macOS and Linux only, where that is the
// lock's name) whose target names a pid that is not alive: Mozilla's fallback lock left
// behind (ADV-L1). Read with Lstat and Readlink only; the link is never followed. A link
// naming another computer is never stale here, whatever its pid (PCT-2): remoteParentLink.
func staleParentLink(profile string) bool {
	target, ok := parentLinkTarget(profile)
	if !ok || lockLinkRemote(target) {
		return false
	}
	pid := lockTargetPID(target)
	return pid > 0 && !procs.Alive(pid)
}

// remoteParentLink reports a `.parentlock` link (macOS and Linux only) naming another
// computer (PCT-2): held, worded as possibly open there.
func remoteParentLink(profile string) bool {
	target, ok := parentLinkTarget(profile)
	return ok && lockLinkRemote(target)
}

// parentLinkTarget is the target of a `.parentlock` link (macOS and Linux only, where that
// is the lock's name); ok false when it is not a link or cannot be read.
func parentLinkTarget(profile string) (string, bool) {
	if ParentLockName != ".parentlock" {
		return "", false
	}
	p := filepath.Join(profile, ParentLockName)
	fi, err := os.Lstat(p)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	target, err := os.Readlink(p)
	return target, err == nil
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
// reused by another program still counts, as presync does. Settled 2026-11-07 [PCT-2]: a
// link naming another computer's address is held whatever its pid (remote true), as
// Mozilla's IsSymlinkStaleLock holds it ("Remote lock: give up even if stuck"); its pid is
// never asked about here.
func symlinkLockHeld(profile string) (held, remote bool) {
	for _, name := range []string{"lock", ".parentlock"} {
		p := filepath.Join(profile, name)
		fi, err := os.Lstat(p)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			continue
		}
		target, err := os.Readlink(p)
		if err != nil {
			held = true
			continue
		}
		if lockLinkRemote(target) {
			return true, true
		}
		if pid := lockTargetPID(target); pid <= 0 || procs.Alive(pid) {
			held = true
		}
	}
	return held, false
}

// lockLinkRemote reports a Mozilla lock link target ("ADDRESS:PID" or "ADDRESS:+PID",
// toolkit/profile/nsProfileLock.cpp LockWithSymlink) whose address is not this computer's
// (PCT-2). The address is what comes before the first ":", as IsSymlinkStaleLock reads it.
// This computer's are a loopback address (127.0.0.0/8, where Mozilla's own falls back) and
// the addresses of its network interfaces; an address that is not a number is this
// computer's only when it is this computer's host name. Anything else is another
// computer's, the refusing side when in doubt (an interface list that cannot be read
// included). A target with no address is judged by its pid, as before.
func lockLinkRemote(target string) bool {
	i := strings.IndexByte(target, ':')
	if i < 0 {
		return false
	}
	addr := strings.TrimSpace(target[:i])
	if addr == "" {
		return false
	}
	return !isThisComputer(addr)
}

// isThisComputer reports whether addr names this computer (lockLinkRemote's rule).
func isThisComputer(addr string) bool {
	ip := net.ParseIP(addr)
	if ip == nil {
		h, err := os.Hostname()
		return err == nil && strings.EqualFold(strings.TrimSuffix(h, "."), strings.TrimSuffix(addr, "."))
	}
	if ip.IsLoopback() {
		return true
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(ip) {
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
