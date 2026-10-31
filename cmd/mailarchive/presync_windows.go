//go:build windows

package main

import (
	"mail-archive-tool/internal/presync"
)

// presyncInstallNote is printed when a scheduled sync is installed or previewed.
// Windows needs no first-approval grant (unlike the macOS Automation prompt): the
// pre-sync asks Thunderbird to close with its own WM_CLOSE, which needs no special
// permission. SyncNotice already carries the running-Thunderbird defer, so the note
// adds only the Windows-specific word on the minimized start (MA-923; lab L-A10 saw
// no window on screen; a window that shows anyway is minimized by the backstop).
func presyncInstallNote(string, bool) string {
	return presync.SyncNotice + "\n\nOn Windows, each scheduled run starts Thunderbird minimized to the taskbar while it syncs; if its window appears anyway, it is minimized."
}
