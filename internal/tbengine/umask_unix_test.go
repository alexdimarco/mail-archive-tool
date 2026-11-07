//go:build !windows

package tbengine

import "syscall"

// setUmask sets the process umask and returns the one before (R7-01's test: a umask of 0
// must not widen what the restore makes).
func setUmask(m int) int { return syscall.Umask(m) }
