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

// presyncInstallNote is printed when a scheduled sync is installed (preview=false)
// or previewed (preview=true), design §8 step 2: the §6 notice of what the sync
// does, then the macOS first-approval guidance (PC4) — the owner runs the installed
// job ONCE now, so macOS attributes the Automation grant to the scheduled program
// (a Terminal run's grant does not count). On the PREVIEW path the schedule does
// not exist yet, so the kickstart command (whose launchd label is not registered)
// is omitted and the grant is framed as a future step — otherwise the owner is
// handed a command that cannot work until a step the same screen says is not done.
// Neither variant prints the raw status code; it states the plain outcome instead.
// Empty off macOS (the schedule-time refusal means a non-macOS schedule can never
// carry the sync).
func presyncInstallNote(name string, preview bool) string {
	const leftOpen = "Until a run the scheduler started has closed Thunderbird, `status` will warn that Thunderbird was left open because macOS has not yet allowed Mail Archive to close it."
	if preview {
		return fmt.Sprintf("%s\n\nAfter you install this schedule, grant permission once: run the installed job while you are at the computer and answer the macOS prompt — Thunderbird will open and close, and if macOS asks whether Mail Archive may control Thunderbird, choose Allow. %s",
			presync.SyncNotice, leftOpen)
	}
	return fmt.Sprintf("%s\n\nTo let the scheduled run control Thunderbird, start it once now while you are at the computer and answer the macOS prompt:\n  launchctl kickstart gui/%d/%s\nThunderbird will open and close; if macOS asks whether Mail Archive may control Thunderbird, choose Allow. %s",
		presync.SyncNotice, os.Getuid(), schedule.LaunchdLabel(name), leftOpen)
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
