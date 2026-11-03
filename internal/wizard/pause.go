package wizard

import (
	"fmt"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
)

// PauseArchive makes the archive at out hard read-only ("Pause (make read-only)", the
// operator-ruled verb). It removes every live scheduler entry this archive owns — the
// backup and, if present, the separate verify — through schedule.RemoveLiveEntry, which
// KEEPS their descriptors so Resume can reinstall; THEN it writes the seal. Order matters
// (S2-C5): schedules go first, so a crash between the steps leaves a writable archive with
// no live schedule (retryable), never a sealed archive whose schedule keeps firing.
//
// Fail-closed: if a live entry cannot be removed, it is NOT sealed — the archive stays as
// it was rather than being marked read-only while a schedule might still run (MA-659).
// reason is recorded in the sentinel for the owner.
// removeLiveEntry is a seam so a test can assert which (bound) names a pause acts on
// without touching the real host scheduler.
var removeLiveEntry = schedule.RemoveLiveEntry

// queryLiveEntry is a seam over the host scheduler's answer about a named entry, so a test of
// AutoPauseIfDue decides what is running without asking the real scheduler.
var queryLiveEntry = schedule.Query

// PauseArchive is Pause (described above). deletionsLog is the deletions.log to ask ("" is
// MailArchive's own): a folder whose delete did
// not finish, or a log that cannot be read, is refused before anything changes (MA-1015; the 1.5.3
// re-walk's F1), so every caller — the Desktop, the terminal's `pause` — gets the check.
func PauseArchive(out, reason, deletionsLog string) error {
	if err := archdelete.ActionRefusal(deletionsLog, out, "it was not paused"); err != nil {
		return err
	}
	// Remove the live entries this archive OWNS, each proven "ours" through bound.go
	// (RecordedBackupName/RecordedVerifyName) exactly as `-out X -remove` and the GUI remove
	// do — NEVER the raw descriptor name, which a handed-over folder can set to a foreign OS
	// task (Windows) or another archive's derived entry. A name that does not bind is skipped
	// (no live entry of ours lives under it); a bound entry that cannot be removed fails
	// closed (we do NOT seal, per the contract above).
	if name, err := schedule.RecordedBackupName(out); err == nil {
		spec := schedule.Spec{Name: name, Out: out, WrapperPath: schedule.DefaultWrapperPath(name)}
		if rerr := removeLiveEntry(spec); rerr != nil {
			return fmt.Errorf("could not remove the backup schedule before pausing (it may still run): %w", rerr)
		}
	}
	if name, ok := schedule.RecordedVerifyName(out); ok {
		spec := schedule.Spec{Name: name, Out: out, WrapperPath: schedule.DefaultWrapperPath(name)}
		if rerr := removeLiveEntry(spec); rerr != nil {
			return fmt.Errorf("could not remove the verify schedule before pausing (it may still run): %w", rerr)
		}
	}
	// An encrypted archive's pause file is a fixed, empty marker: the owner's note
	// would sit beside the archive in plaintext (phase2a §4). The note returns in
	// Phase 3 inside the sealed record (owner ruling 2026-10-25; MA-662 unchanged).
	// A folder INSIDE an encrypted archive is not paused at all: its pause file (and
	// the folder, if it is missing) would be written into the encrypted archive. When
	// the check cannot tell, the archive is treated as encrypted: the marker is empty
	// (the Phase-2b-1 adversarial pass, FC-4).
	dir, _, derr := encmark.AtOrAbove(out)
	if derr != nil {
		reason = ""
	}
	if dir != "" {
		if !app.ArchiveEncrypted(out) {
			return fmt.Errorf("%w: %s is inside the encrypted archive %s — pause the archive itself", app.ErrEncryptedNeedsKey, out, dir)
		}
		reason = ""
	}
	return seal.Seal(out, reason)
}

// ResumeArchive reverses Pause ("Resume"). It clears the seal FIRST (S2-C5: never leave a
// sealed archive with a live schedule), then reinstalls the backup schedule from its kept
// descriptor through the same secure repair path the dashboard and wizard use
// (repairSchedule: the cadence comes from the descriptor, the job args are rebuilt from the
// sanitized name + -out, never the descriptor's recorded Job/Exe). A scheduled verify is
// not auto-restored in this release — its descriptor is kept and the owner can re-enable it.
// A folder whose delete did not finish, by deletionsLog ("" is MailArchive's own), stays paused
// and gets no schedule back, as for PauseArchive (MA-1015; the 1.5.3 re-walk's F1).
func ResumeArchive(out, deletionsLog string) error {
	if err := archdelete.ActionRefusal(deletionsLog, out, "it was not resumed"); err != nil {
		return err
	}
	if err := seal.Unseal(out); err != nil {
		return err
	}
	// A backup run only when the person clicks Run now had no entry for Pause to stop, so
	// Resume puts none back (MA-949).
	if d, err := schedule.ReadDescriptor(out); err == nil && !d.IsManual() {
		if rerr := repairSchedule(out, d, deletionsLog); rerr != nil {
			return fmt.Errorf("the archive is writable again, but its backup schedule could not be restored (re-install it from Settings): %w", rerr)
		}
	}
	return nil
}

// AutoPauseIfDue stops the archive's backup schedule when its SOURCE's unreachable runs span
// state.AutoPauseAfter (30 days, measured between runs; §5.7, S3-C5,
// design-desktop-settings-card 3.7), only to cut failure noise. It removes ONLY the live scheduler entry (the backup and, if present, the
// verify), each proven "ours" through bound.go — never the raw descriptor name (MA-680) — and
// KEEPS the descriptor, so it is fully reversible: reinstalling the schedule (Resume, or a
// reconnect) brings it back. It NEVER seals and NEVER forgets the archive. A scheduled run
// calls it after it finishes (so the streak it reads is up to date), saying whether that run
// itself found the source unreachable (app.FoundSourceUnreachable): only such a run pauses, so
// a failure that says nothing about the source never pauses on an old streak a re-enable left
// (MA-890). It reports paused only when it removed this archive's own backup entry and the
// scheduler said that entry was installed just before — with no bound entry (no readable
// descriptor, or one that is not this archive's), or none running under the bound name (an
// earlier auto-pause already stopped it), nothing was stopped, so it says so in its error and the
// caller sends no auto-pause email (MA-890). The verify is stopped only with such a backup. Below
// the threshold it does nothing.
func AutoPauseIfDue(out string, runFoundSourceUnreachable bool) (bool, error) {
	if !runFoundSourceUnreachable {
		return false, nil
	}
	streak, err := state.ReadSourceStreak(out)
	if err != nil || !streak.AutoPauseDue() {
		return false, nil
	}
	name, nerr := schedule.RecordedBackupName(out)
	if nerr != nil {
		return false, fmt.Errorf("auto-pause is due, but this archive records no backup schedule of its own to stop (%v), so nothing was paused", nerr)
	}
	if paused, err := stopLiveBackup(name, out); !paused {
		return false, err
	}
	// The verify goes only with a backup this pause stopped: a "nothing was paused" result never
	// leaves a verify stopped with no email (the part A re-verification).
	if vname, ok := schedule.RecordedVerifyName(out); ok {
		if rerr := removeLiveEntry(schedule.Spec{Name: vname, Out: out, WrapperPath: schedule.DefaultWrapperPath(vname)}); rerr != nil {
			return true, rerr
		}
	}
	return true, nil
}

// stopLiveBackup stops the live scheduler entry name, which backs up into out, and reports whether
// it stopped one that was running. Only an entry the scheduler said was there is stopped: the
// removal succeeds on a missing entry on every system, so "removed" alone is not "was running". A
// scheduler that can't answer (Windows' SchedulerUnavailable) is not "installed", so it never
// reports a pause. The removal itself stays unconditional, so a missing entry's leftover side
// files still go. It never touches out: the entry, and on Windows its side files beside the
// wrapper, are this computer's.
func stopLiveBackup(name, out string) (bool, error) {
	wasLive := queryLiveEntry(name) == schedule.Installed
	if rerr := removeLiveEntry(schedule.Spec{Name: name, Out: out, WrapperPath: schedule.DefaultWrapperPath(name)}); rerr != nil {
		return false, rerr
	}
	if !wasLive {
		return false, fmt.Errorf("auto-pause is due, but no live backup schedule was running under %s, so nothing was paused", name)
	}
	return true, nil
}

// AutoPauseForDeleteIfDue is the auto-pause for a delete this computer never began
// (docs/design-archive-delete.md §15.10 slice 6, the owner's ruling Q5, DM-16; MA-1058). A
// scheduled run calls it with its own outcome runErr (nil for a run that got past the delete
// check): a run refused delete_unfinished for a delete only the folder's marker knows, whose run
// this computer's deletion log at deletionsLog ("" is MailArchive's own) never began
// (archdelete.NeverBegunHere), extends the count of such runs kept for key beside the settings
// file at settingsPath (backupalert.NoteDeleteRefused); any other outcome ends it. Once the refused
// runs span state.AutoPauseAfter (30 days, at least two runs: the source-loss rule) it stops this
// computer's live scheduler entry named entry — the job file's name for a local job, the name the
// Desktop installed its capture under — as the source-loss auto-pause does (stopLiveBackup), and
// reports paused only when that entry was running. It writes nothing into out and never seals: the
// folder holds a half-deleted archive, and its descriptor may already be gone, so the entry is named
// by this computer, not by the folder. A delete this computer's log knows never counts: its row
// already says "click Delete again". The caller sends the auto-pause email
// (backupalert.RecordAutoPause, which also ends the count).
func AutoPauseForDeleteIfDue(settingsPath, key, entry, out, deletionsLog string, runErr error, now time.Time) (bool, error) {
	refused := runErr != nil && app.FailureKind(runErr) == state.FailDeleteUnfinished && archdelete.NeverBegunHere(deletionsLog, out)
	count, err := backupalert.NoteDeleteRefused(settingsPath, key, refused, now)
	if err != nil {
		return false, fmt.Errorf("could not keep the count of backups refused for this archive's unfinished delete: %w", err)
	}
	if !refused || !count.AutoPauseDue() {
		return false, nil
	}
	name, nerr := schedule.SanitizeName(entry)
	if nerr != nil || name != entry {
		return false, fmt.Errorf("auto-pause is due, but %q is not a backup schedule's name, so nothing was paused", entry)
	}
	return stopLiveBackup(name, out)
}

// SetSchedulerForTest replaces the host scheduler the auto-pauses ask and stop (query, remove) and
// returns the function that puts the real one back: another package's test (the Desktop's scheduled
// capture) drives a real auto-pause without touching this computer's scheduler.
func SetSchedulerForTest(query func(string) schedule.State, remove func(schedule.Spec) error) (restore func()) {
	oq, or := queryLiveEntry, removeLiveEntry
	queryLiveEntry, removeLiveEntry = query, remove
	return func() { queryLiveEntry, removeLiveEntry = oq, or }
}
