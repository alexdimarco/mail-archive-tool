//go:build !windows

// Package procs answers one question the status surfaces need: is a recorded
// pid still alive? (A "running" last-run record whose pid is dead is a run
// that never finished.)
package procs

import (
	"errors"
	"syscall"
)

// Alive reports whether a process with pid exists. EPERM means it exists but
// belongs to someone else — still alive.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
