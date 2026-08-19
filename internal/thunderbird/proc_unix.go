//go:build !windows

package thunderbird

import "syscall"

// pidAlive reports whether the process is alive. Signal 0 performs error
// checking without delivering a signal: nil means alive; EPERM means alive but
// owned by another user; ESRCH means no such process (dead/stale lock).
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
