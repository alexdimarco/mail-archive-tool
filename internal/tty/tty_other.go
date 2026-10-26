//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly && !windows

package tty

import (
	"io"
	"os"
)

// IsTerminal reports false on a platform this build has no terminal check for: a
// link is then printed only where stdout is known to be a terminal, i.e. never.
func IsTerminal(f *os.File) bool { return false }

// Foreground reports false for the same reason: nothing reads a terminal.
func Foreground(f *os.File) bool { return false }

// ReadSecret refuses: with no terminal check, no secret is asked for.
func ReadSecret(in *os.File, out io.Writer, prompt string) ([]byte, error) {
	return nil, ErrNotTerminal
}
