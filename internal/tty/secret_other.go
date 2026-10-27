//go:build !windows

package tty

// interruptGrace is zero here: a terminal's Ctrl-C is a signal that never ends a read
// as input that ran out, so a prompt has nothing to wait for (Windows waits; see
// secret_windows.go).
const interruptGrace = 0
