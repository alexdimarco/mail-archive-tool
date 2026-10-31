package health

import (
	"runtime"

	"mail-archive-tool/internal/presync"
)

// A backup run only when the person clicks Run now (How often: "Only when I click Run now",
// owner ruling 2026-10-31, 1.5.2; MA-949, MA-950) is the person's choice, not a missing or
// broken schedule: its descriptor records the choice, its job file is kept on this computer,
// and no scheduler entry exists. status and the Desktop's Status & health say so in a note,
// and none of a schedule's checks apply to it: not installed, the scheduled program, stale,
// never ran, and the Thunderbird sync's 3-day warnings, which count scheduled sessions only.

// manualBackup reports a backup the archive's descriptor records as run only on Run now.
func manualBackup(in Input) bool { return in.HasDescriptor && in.Desc.IsManual() }

// manualBackupNote is the note for such a backup, in the words of the surface reading it.
func manualBackupNote(in Input) string {
	s := "this archive is backed up only when you click Run now"
	switch in.Surface {
	case "desktop":
		s += " on the Scheduled backup card"
	default:
		s += " in MailArchive Desktop"
	}
	// No `mailarchive run-job JOBFILE` here: run-job is the scheduled path (it sends the scheduled
	// backup's alerts and counts the 3-day sync streak), so it is not the same backup as Run now.
	return s + ": that is its How often setting (Only when I click Run now), so nothing runs by itself and no scheduled backup is expected"
}

// manualJobMissing is the warning for a descriptor that records such a backup while its job
// file is not on this computer (owner-side evidence, never the archive's record alone): Run
// now has nothing to run.
func manualJobMissing(surface string) string {
	s := "this archive is set to back up only when you click Run now, but its backup settings file is not on this computer, so Run now has nothing to run — "
	if surface == "desktop" {
		return s + "the Scheduled backup card says how to set it up again"
	}
	return s + "open MailArchive Desktop on this computer: its Scheduled backup card says how to set it up again"
}

// manualEntryRunning is the warning for a descriptor that records such a backup while the
// scheduler still holds an entry for the archive (its removal skipped when the scheduler could
// not answer, or the entry put back by hand): the entry runs the backup by itself. Saving How
// often again takes the entry out, or refuses in plain words when it can't.
func manualEntryRunning(surface string) string {
	s := "a scheduler entry still runs this archive's backup by itself, although its How often is Only when I click Run now — "
	if surface != "desktop" {
		s += "open MailArchive Desktop on this computer and, in Settings, "
	} else {
		s += "in Settings, "
	}
	return s + "choose Only when I click Run now and click Save settings again to take the entry out"
}

// manualSyncNote is the presync_syncs note for such a backup with the Thunderbird sync on: it
// syncs when Run now starts Thunderbird on this platform (presync.AttendedStartAllowed), and
// elsewhere says what to do instead, so it never promises a sync Run now does not do.
func manualSyncNote(goos string) string {
	if goos == "" {
		goos = runtime.GOOS
	}
	if presync.AttendedStartAllowed(goos) {
		return "this archive's backup syncs Thunderbird first each time you click Run now: it starts Thunderbird, even while you are using the computer, waits for its mail, then backs up, and leaves Thunderbird open for you to quit"
	}
	return "this archive's backup is set to sync Thunderbird first, but on this computer Run now does not start Thunderbird yet: open Thunderbird and let its mail arrive before you click Run now"
}

// manualScheduleLine is status's Schedule line for such a backup.
const manualScheduleLine = "Schedule:   none — backed up only when you click Run now in MailArchive Desktop (How often: Only when I click Run now); nothing runs by itself"
