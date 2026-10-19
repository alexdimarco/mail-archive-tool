//go:build darwin || linux

package main

import (
	"os"

	"mail-archive-tool/internal/tty"
)

// noTerminal reports that none of stdin, stdout and stderr is a terminal and
// that /dev/tty cannot be opened — the macOS/Linux test for a start without a
// terminal (Finder, Dock, `open`, a systemd unit, a cron job). /dev/tty is
// opened and closed at once and never read, so the check has no side effect
// (§4.3, SC16). It is the same terminal source cmd/mailarchive/link.go uses.
func noTerminal() bool {
	if tty.IsTerminal(os.Stdin) || tty.IsTerminal(os.Stdout) || tty.IsTerminal(os.Stderr) {
		return false
	}
	f, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		return true
	}
	_ = f.Close()
	return false
}
