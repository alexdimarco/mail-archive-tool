package presync

import "time"

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
}
