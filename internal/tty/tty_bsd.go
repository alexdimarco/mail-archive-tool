//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package tty

import (
	"os"

	"golang.org/x/sys/unix"
)

// The ioctls that read and set a terminal's settings.
const (
	ioctlGetTermios = unix.TIOCGETA
	ioctlSetTermios = unix.TIOCSETA
)

// IsTerminal reports whether f is a terminal (a tty/pty), not a pipe, a file or
// /dev/null.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	_, err := unix.IoctlGetTermios(int(f.Fd()), unix.TIOCGETA)
	return err == nil
}
