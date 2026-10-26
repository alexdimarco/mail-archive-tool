//go:build !windows

package tty

import "os"

// CanErase reports whether f is a terminal that can show the recovery key on a
// screen of its own and take it away again (the alternate screen): any terminal
// but one that says it is dumb.
func CanErase(f *os.File) bool { return IsTerminal(f) && os.Getenv("TERM") != "dumb" }
