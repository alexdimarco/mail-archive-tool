//go:build windows

package main

import (
	"path/filepath"

	"mail-archive-tool/internal/instance"
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

// presyncStateDir is the "presync" subdirectory of the per-user, NON-ROAMING state
// directory (design §4.6, PC-W5): the pinned instance.Dir helper (MA-621) — the
// per-user local app-data folder, not the roaming one — so a live pid in the
// write-ahead record never travels to another machine, which jobfile.Dir (which
// roams on Windows) would otherwise allow.
func presyncStateDir() (string, error) {
	dir, err := instance.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "presync"), nil
}
