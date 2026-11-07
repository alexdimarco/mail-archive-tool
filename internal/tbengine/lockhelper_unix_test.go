//go:build !windows

package tbengine

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// takeThunderbirdLock takes the POSIX record lock Mozilla takes on .parentlock (F_SETLK,
// whole file, write lock), as a starting Thunderbird would, and lets it go.
func takeThunderbirdLock(path string) string {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return "open-failed"
	}
	defer f.Close()
	fl := unix.Flock_t{Type: unix.F_WRLCK}
	if err := unix.FcntlFlock(f.Fd(), unix.F_SETLK, &fl); err != nil {
		return "refused"
	}
	return "took"
}
