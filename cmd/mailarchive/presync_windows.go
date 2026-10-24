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
// permission. So the note is just the §6 description of what the sync does, plus a
// word on the visible-then-minimized launch and the running-Thunderbird defer.
func presyncInstallNote(string) string {
	return presync.SyncNotice + "\n\nAt each scheduled run Thunderbird opens (then minimizes to the taskbar), syncs, and is asked to close; if Thunderbird is already open it is left alone, never started or closed."
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
