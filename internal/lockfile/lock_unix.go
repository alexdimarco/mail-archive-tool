//go:build !windows

package lockfile

import (
	"os"
	"syscall"
)

// noFollow refuses to open through a symlink (the O_NOFOLLOW open flag).
const noFollow = syscall.O_NOFOLLOW

// tryLock takes an exclusive, non-blocking flock. flock is per open file
// description, so a second Acquire in the same process fails too, and the OS
// releases it when the descriptor closes or the process dies.
func tryLock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
