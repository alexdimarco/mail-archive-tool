//go:build linux

package tty

import (
	"os"

	"golang.org/x/sys/unix"
)

// The ioctls that read and set a terminal's settings.
const (
	ioctlGetTermios = unix.TCGETS
	ioctlSetTermios = unix.TCSETS
)

// IsTerminal reports whether f is a terminal (a tty/pty), not a pipe, a file or
// /dev/null.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	_, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	return err == nil
}
