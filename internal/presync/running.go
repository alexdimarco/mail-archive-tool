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
}

// detectRunning evaluates signals A and B (design 4.4.1). Thunderbird is started
// only when neither fires. A false positive fails safe (no sync this run, the
// owner is told); a false negative costs at most the max wait.
func detectRunning(e env, profile string) runningSignal {
	var s runningSignal
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
