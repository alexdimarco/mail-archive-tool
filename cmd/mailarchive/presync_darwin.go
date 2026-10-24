//go:build darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"

	jobfile "mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/schedule"
)

// presyncInstallNote is printed when a scheduled sync is installed or previewed
// (design §8 step 2): the §6 notice of what the sync does, then the macOS
// first-approval command (PC4) — the owner runs the installed job ONCE now, so
// macOS attributes the Automation grant to the scheduled program (a Terminal
// run's grant does not count). Empty off macOS (the schedule-time refusal means
// a non-macOS schedule can never carry the sync).
func presyncInstallNote(name string) string {
	return fmt.Sprintf("%s\n\nTo let the scheduled run control Thunderbird, start it once now while you are at the computer and answer the macOS prompt:\n  launchctl kickstart gui/%d/%s\nThunderbird will open and close; if macOS asks whether Mail Archive may control Thunderbird, choose Allow. Until a scheduler-started run has closed Thunderbird, `status` shows presync_quit_not_permitted.",
		presync.SyncNotice, os.Getuid(), schedule.LaunchdLabel(name))
}

// presyncStateDir is the "presync" subdirectory of the job files' per-user
// directory (design §4.6): the write-ahead record and the session lock live
// here, never in the archive. It derives from the existing, pinned per-user
// location (jobfile.Dir, MA-621) rather than computing a new one.
func presyncStateDir() (string, error) {
	dir, err := jobfile.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "presync"), nil
}
