//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package tty

import (
	"os"

	"golang.org/x/sys/unix"
)

// Foreground reports whether this process is in the foreground process group of
// the terminal f: tcgetpgrp(f) equals getpgrp(). A process that reads a terminal
// from the background is stopped by SIGTTIN, so `serve &` must not read stdin
// until it is brought back with `fg` [CC18]; it is checked before every read.
// When f is not a terminal there is no foreground to be outside of: it reports
// false, so no caller reads a terminal it does not have.
func Foreground(f *os.File) bool {
	if f == nil {
		return false
	}
	pg, err := unix.IoctlGetInt(int(f.Fd()), unix.TIOCGPGRP)
	if err != nil {
		return false
	}
	return pg == unix.Getpgrp()
}
