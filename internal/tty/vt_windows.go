//go:build windows

package tty

import (
	"os"

	"golang.org/x/sys/windows"
)

// CanErase reports whether f is a console that can show the recovery key on a
// screen of its own and take it away again: one that accepts terminal sequences,
// turned on here if it can be.
func CanErase(f *os.File) bool {
	if !IsTerminal(f) {
		return false
	}
	h := windows.Handle(f.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
