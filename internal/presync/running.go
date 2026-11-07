package presync

import (
	"path/filepath"
	"strings"
)

// runningSignal is why the start gate decided Thunderbird is (or is not) running
// (design section 4.4.1). Running = A (the profile lock names a live pid) OR B
// (a Thunderbird-named process). A live lock pid that is NOT Thunderbird is a
// stale lock whose pid was reused — still treated as running (Mozilla refuses
// such a profile), and flagged.
type runningSignal struct {
	running   bool
	byLock    bool
	byProcess bool
	lockNotTB bool
	// byRestore: a MailArchive restore into this profile holds its restore lock
	// (docs/design-restore-thunderbird.md §5.2 note item 3): the profile is in use.
	byRestore bool
}

// detectRunning evaluates signals A and B (design 4.4.1), and first the MailArchive
// restore lock (design-restore-thunderbird §5.2 note item 3, slice R3): a restore
// writing into the profile counts as Thunderbird in use, so the pre-sync never
// starts Thunderbird on it meanwhile. Thunderbird is started only when none fires.
// A false positive fails safe (no sync this run, the owner is told); a false
// negative costs at most the max wait.
func detectRunning(e env, profile string) runningSignal {
	var s runningSignal
	if e.restoreLockHeld(profile) {
		s.byRestore, s.running = true, true
	}
	if pid, present := e.lockPID(profile); present {
		switch {
		case pid > 0 && e.pidAlive(pid):
			s.byLock, s.running = true, true
			if exe, ok := e.procExe(pid); ok && !isThunderbirdExe(exe) {
				s.lockNotTB = true // a stale lock whose pid was reused
			}
		case pid <= 0:
			// a lock that names no parseable pid is treated as held (Mozilla does)
			s.byLock, s.running = true, true
		}
		// a lock naming a dead pid is stale: not a running signal, so fall to B
	}
	if len(e.thunderbirdProcs()) > 0 {
		s.byProcess, s.running = true, true
	}
	return s
}

// isThunderbirdExe reports whether an executable path is a Thunderbird binary,
// by its base name (design 4.4.1).
func isThunderbirdExe(exe string) bool {
	switch strings.ToLower(filepath.Base(exe)) {
	case "thunderbird", "thunderbird-bin", "thunderbird.exe":
		return true
	}
	return false
}
