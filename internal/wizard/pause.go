package wizard

import (
	"fmt"

	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
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
func PauseArchive(out, reason string) error {
	if d, err := schedule.ReadDescriptor(out); err == nil {
		spec := schedule.Spec{Name: d.Name, Out: out, WrapperPath: schedule.DefaultWrapperPath(d.Name)}
		if rerr := schedule.RemoveLiveEntry(spec); rerr != nil {
			return fmt.Errorf("could not remove the backup schedule before pausing (it may still run): %w", rerr)
		}
	}
	if dv, err := schedule.ReadVerifyDescriptor(out); err == nil {
		spec := schedule.Spec{Name: dv.Name, Out: out, WrapperPath: schedule.DefaultWrapperPath(dv.Name)}
		if rerr := schedule.RemoveLiveEntry(spec); rerr != nil {
			return fmt.Errorf("could not remove the verify schedule before pausing (it may still run): %w", rerr)
		}
	}
	return seal.Seal(out, reason)
}

// ResumeArchive reverses Pause ("Resume"). It clears the seal FIRST (S2-C5: never leave a
// sealed archive with a live schedule), then reinstalls the backup schedule from its kept
// descriptor through the same secure repair path the dashboard and wizard use
// (repairSchedule: the cadence comes from the descriptor, the job args are rebuilt from the
// sanitized name + -out, never the descriptor's recorded Job/Exe). A scheduled verify is
// not auto-restored in this release — its descriptor is kept and the owner can re-enable it.
func ResumeArchive(out string) error {
	if err := seal.Unseal(out); err != nil {
		return err
	}
	if d, err := schedule.ReadDescriptor(out); err == nil {
		if rerr := repairSchedule(out, d); rerr != nil {
			return fmt.Errorf("the archive is writable again, but its backup schedule could not be restored (re-install it from Settings): %w", rerr)
		}
	}
	return nil
}
