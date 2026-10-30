//go:build windows

package main

import (
	"mail-archive-tool/internal/presync"
)

// presyncInstallNote is printed when a scheduled sync is installed or previewed.
// Windows needs no first-approval grant (unlike the macOS Automation prompt): the
// pre-sync asks Thunderbird to close with its own WM_CLOSE, which needs no special
// permission. SyncNotice already carries the running-Thunderbird defer, so the note
// adds only the Windows-specific word on the visible-then-minimized launch.
func presyncInstallNote(string, bool) string {
	return presync.SyncNotice + "\n\nOn Windows, each scheduled run briefly shows the Thunderbird window, then minimizes it to the taskbar while it syncs."
}
