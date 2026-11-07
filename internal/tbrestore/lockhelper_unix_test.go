//go:build !windows

package tbrestore

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// lockHelper is the child's side: the classic POSIX record lock Mozilla takes on
// .parentlock (F_GETLK, then F_SETLK, whole file, write lock), so the child stands for
// a starting Thunderbird. "getlk" answers held or free; "take" and "hold" try F_SETLK
// and answer took or refused (hold then keeps it).
func lockHelper(mode, path string) string {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return "open-failed " + err.Error()
	}
	fl := unix.Flock_t{Type: unix.F_WRLCK}
	switch mode {
	case "getlk":
		defer f.Close()
		if err := unix.FcntlFlock(f.Fd(), unix.F_GETLK, &fl); err != nil {
			return "error " + err.Error()
		}
		if fl.Type == unix.F_UNLCK {
			return "free"
		}
		return "held"
	case "take", "hold":
		if err := unix.FcntlFlock(f.Fd(), unix.F_SETLK, &fl); err != nil {
			f.Close()
			return "refused"
		}
		if mode == "take" {
			f.Close()
		}
		return "took"
	}
	return "unknown mode"
}
