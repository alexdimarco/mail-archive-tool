package presync

import (
	"context"
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

// errQuitNotPosted is returned by quit when it asked nothing at all: on Windows,
// every "cannot safely close" path (no handle held, a creation-time mismatch, zero
// or several main-like windows, the last-instant re-check failing, a post that did
// not take) posts no WM_CLOSE (design-presync-attended A10 step 7, C-60). The
// machine maps it at once to presync_quit_no_window and leaves Thunderbird
// running, with no exit-grace wait and no text saying it was asked to close.
var errQuitNotPosted = errors.New("quit not posted")

// proc is a snapshot of one Thunderbird-relevant process (design sections 4.4.1,
// 4.4.3): its pid, resolved executable path, start time (UTC) and command line
// (which carries the -profile argument).
type proc struct {
	pid   int
	exe   string
	start time.Time
	args  string
	// argv is the command line already split into arguments, when the adapter can
	// supply a faithful split (Windows, where the raw command line is quoted and a
	// user folder may contain spaces). It is nil on macOS, where ownership splits
	// args on whitespace — see profileMatches.
	argv []string
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

	// sleep waits for d, returning early when ctx or stop ends (either may be nil).
	// The real seam is sleepCtx; the fake advances its clock (and does not move it
	// when either context is already done), so the polling loops are deterministic
	// in tests and no wait outlasts a cancel or an abandonment (A5).
	sleep(ctx, stop context.Context, d time.Duration)
	// bootTime is the machine's boot time in unix seconds (a reboot clears the
	// record, design 4.6). procExe/thunderbirdProcs and this never read mail.
	bootTime() int64
	// idleSeconds is how long the owner has been away from keyboard and mouse,
	// and whether it could be read (design 4.4.3 S4; unreadable counts as active).
	idleSeconds() (seconds float64, ok bool)
	// recheckProfile re-resolves the profile's canonical path immediately before
	// the S5 launch (the §5.2 TOCTOU guard): false when it errors or no longer
	// equals the canonical path validated at S1, so a profile swapped under us
	// between resolution and launch is refused rather than started.
	recheckProfile(canonical string) bool
	// startHidden launches the application out of sight on the profile (S5). The
	// real seam execs the platform launcher; it never blocks on the GUI.
	startHidden(app, profile string) error
	// quit asks the proven child Thunderbird to quit through its own quit request
	// (S8). It never signals or kills. The target is the proven child: macOS
	// ignores it and quits by bundle id, but Windows needs its pid to post
	// WM_CLOSE to that process's own window (PC-W2), so the session hands it over.
	// ctx carries the request's deadline, which the session builds from its own
	// context, so a Lock or an abandonment ends a request in flight (C-5).
	quit(ctx context.Context, target proc) error
	// quitAffectsAllInstances reports whether quit closes EVERY Thunderbird of the
	// bundle (macOS: the request goes to the app by bundle id) rather than the one
	// proven pid (Windows: WM_CLOSE to that pid's window). When true, S8 skips the quit
	// if more than one Thunderbird is running, so the owner's is never closed too (C3).
	quitAffectsAllInstances() bool
	// rawThunderbirdCount is how many Thunderbird-named processes the OS lists for
	// this user, counting those whose details cannot be read (C-1/SAF-9): the C3
	// count, so a process thunderbirdProcs drops is still counted as a second
	// Thunderbird. ok is false when the list itself could not be read.
	rawThunderbirdCount() (n int, ok bool)
	// digest folds the watch set into one value without opening a file (S7); the
	// real seam calls digestWatch.
	digest(roots []string) (sum uint64, truncated bool)
	// acquireLock takes the per-profile session lock (S2); released at END.
	acquireLock(path string) (ok bool, err error)
	releaseLock(path string)
}
