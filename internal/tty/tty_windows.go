//go:build windows

package tty

import (
	"os"

	"golang.org/x/sys/windows"
)

// IsTerminal reports whether f is a console, not a pipe or a file.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(f.Fd()), &mode) == nil
}

// Foreground is always true on Windows: there are no background process groups
// that a console read stops, so a console process may always read its stdin.
func Foreground(f *os.File) bool { return IsTerminal(f) }
