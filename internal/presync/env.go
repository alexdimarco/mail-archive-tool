package presync

import (
	"errors"
	"time"
)

// errNoDesktopSession is returned by startHidden when the launcher reports no
// usable GUI session (design §4.7.1 / MA-571: the window server is unreachable,
// e.g. at the login window). The state machine maps it to
// OutcomeNoDesktopSession rather than the generic cannot-start, and does not
// retry. It is declared here, platform-neutral, so the machine can classify it
// without a build tag.
var errNoDesktopSession = errors.New("no desktop session")

// errQuitNotPermitted is returned by quit when the quit request did not take —
// macOS Automation consent denied (osascript -1743), the Apple event timing out
// (-1712) or our own deadline firing (design §4.7.1 / MA-570). The likely cause
// is an unanswered first-approval prompt; the machine leaves Thunderbird running
// and records a leftover rather than forcing it. Platform-neutral so the machine
// classifies it without a build tag.
var errQuitNotPermitted = errors.New("quit not permitted")

// proc is a snapshot of one Thunderbird-relevant process (design sections 4.4.1,
// 4.4.3): its pid, resolved executable path, start time (UTC) and command line
// (which carries the -profile argument).
type proc struct {
	pid   int
	exe   string
	start time.Time
	args  string
}

// env is the injected OS seam. TP1 drives the pure core with a fake; the real
// macOS implementation (TP3) execs fixed system tools by literal name. Methods
// are added as the pieces that need them land; this set covers the clock,
// running-detection and ownership.
type env interface {
	now() time.Time
	// thunderbirdProcs lists Thunderbird-named processes run by the current user
	// (design 4.4.1 signal B; the 4.4.3 ownership candidates).
	thunderbirdProcs() []proc
	// lockPID reads the profile lock: present is false when there is no lock; pid
	// is <= 0 when the lock names no parseable pid (design 4.4.1 signal A).
	lockPID(profile string) (pid int, present bool)
	// pidAlive reports whether pid is a live process.
	pidAlive(pid int) bool
	// procExe resolves a live pid's executable path, to tell a stale lock whose
	// pid was reused by a non-Thunderbird process.
	procExe(pid int) (exe string, ok bool)

	// sleep waits for d. The real seam blocks; the fake advances its clock, so
	// the state machine's polling loops are deterministic in tests.
	sleep(d time.Duration)
	// bootTime is the machine's boot time in unix seconds (a reboot clears the
	// record, design 4.6). procExe/thunderbirdProcs and this never read mail.
	bootTime() int64
	// idleSeconds is how long the owner has been away from keyboard and mouse,
	// and whether it could be read (design 4.4.3 S4; unreadable counts as active).
	idleSeconds() (seconds float64, ok bool)
	// startHidden launches the application out of sight on the profile (S5). The
	// real seam execs the platform launcher; it never blocks on the GUI.
	startHidden(app, profile string) error
	// quit asks the running Thunderbird to quit through its own quit request (S8).
	// It never signals or kills.
	quit() error
	// digest folds the watch set into one value without opening a file (S7); the
	// real seam calls digestWatch.
	digest(roots []string) (sum uint64, truncated bool)
	// acquireLock takes the per-profile session lock (S2); released at END.
	acquireLock(path string) (ok bool, err error)
	releaseLock(path string)
}
