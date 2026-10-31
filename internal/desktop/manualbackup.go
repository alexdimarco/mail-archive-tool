package desktop

import (
	"net/http"
	"strings"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/wizard"
)

// "Only when I click Run now" (owner ruling 2026-10-31: "It may be fine to have a manual archive
// where someone updates at their own schedule when they desire to do so", then "yes, 1.5.2";
// MA-949): How often's choice for an archive of mail files that saves its backup — the job file
// Run now runs, and the archive's descriptor recording the choice (schedule.DescribeManual) —
// with no scheduler entry, as an encrypted archive's backup already is. Run now runs it, with
// the Thunderbird sync when it is ticked; nothing ever runs by itself. Settings offers it
// wherever it edits a run-job (macOS and Windows); Remove removes the job file and the record.

// The words of a manual backup (X2).
const (
	manualOptionLabel = "Only when I click Run now"
	// manualHint is shown under How often while it is chosen.
	manualHint = "Nothing runs by itself: the backup runs each time you click Run now on the Scheduled backup card. The time is not used."
	// manualKindLine is the Scheduled backup card's line for it, in place of a schedule's kind.
	manualKindLine = "This archive's backup runs only when you click Run now: nothing runs by itself. It re-reads this archive's mail files and adds anything new."
	// manualStateLine is the card's state line for it, in place of "Not installed." (api/schedule-state).
	manualStateLine = "Runs only when you click Run now."
	// manualAlertsLine is the Backup alerts card's line for it: alerts are about scheduled backups.
	manualAlertsLine = "This archive backs up only when you click Run now, so no alert is sent about it: Run now shows how each backup went, and Recent backups keeps the list."
	// manualSyncStartOffNote is said when the sync is saved with it where Run now does not start
	// Thunderbird yet (presync.AttendedStartAllowed), so the save never promises a sync Run now
	// does not do.
	manualSyncStartOffNote = "On this computer Run now does not start Thunderbird yet: open Thunderbird and let its mail arrive before you click Run now."
)

// manualSave is one save of "Only when I click Run now", as scheduleInstallLocal worked it out:
// every refusal is already behind it.
type manualSave struct {
	out, name, jobPath string
	j                  job.Job // the job file to write: the one there, edited, or rebuilt on a create
	exists             bool    // a job file was there to edit
	syncChanged        bool
	wantSync           bool
	recreate           bool // a broken run-job's Repair, its job file missing
	at                 string
}

// saveManual saves "Only when I click Run now" for the archive (MA-949), in the order that never
// leaves a scheduler entry running beside a record that says nothing runs by itself:
//
//  1. the job file, on a create or when the sync changed (job.Write renames a finished copy over
//     the old one, so a failure changes nothing);
//  2. the scheduler entry, when one is running for this archive, is taken out (its record is
//     kept until step 3); a failure here leaves the schedule running as it was, and says so;
//  3. the archive's descriptor, recording the choice.
//
// A switch to it also ends the archive's alert streak and its Thunderbird sync streaks: they
// count scheduled runs, and none will come to end them.
func (d *dashboard) saveManual(w http.ResponseWriter, m manualSave) {
	reply := func(msg string) { writeJSON(w, map[string]any{"ok": false, "error": msg}) }
	wasLive := d.cfg.taskState(m.out) == schedule.Installed
	if !m.exists || m.syncChanged {
		if err := writeJob(m.jobPath, m.j); err != nil {
			reply("The backup could not be saved: its settings file could not be written (" + err.Error() + "). Nothing was changed.")
			return
		}
	}
	if wasLive {
		if err := d.undoInstall(m.out, false); err != nil {
			msg := "The backup schedule was not changed: its scheduler entry could not be taken out (" + err.Error() + "), so it still runs as before."
			if m.exists && m.syncChanged {
				msg += " Thunderbird sync was changed."
			}
			reply(msg)
			return
		}
	}
	spec := wizard.RunJobSpec(m.out, m.name, m.jobPath, schedule.Daily, manualTime(m.at), d.manualExe())
	if err := writeDescriptor(m.out, schedule.DescribeManual(spec)); err != nil {
		switch {
		case !m.exists:
			_ = schedule.RemoveJobFile(m.jobPath) // no half-made backup: Run now would show for a job nothing records
			reply("The backup could not be set up: this archive's record of it could not be written (" + err.Error() + "). Nothing was set up.")
		case wasLive:
			reply("The scheduled backup was stopped, but this archive's record could not be written (" + err.Error() + "): Settings still shows its old schedule. Choose " + manualOptionLabel + " and click Save settings again.")
		default:
			msg := "The backup schedule was not changed: this archive's record could not be written (" + err.Error() + ")."
			if m.syncChanged {
				msg += " Thunderbird sync was changed."
			}
			reply(msg)
		}
		return
	}
	if m.syncChanged {
		syncSwitched(m.out, m.wantSync)
	}
	_ = state.RemoveSyncStreak(m.out) // its 3-day warnings count scheduled sessions only
	d.cfg.clearAlertStreak(m.out)     // no scheduled run will end a failing streak
	var notes []string
	if m.recreate && !m.exists {
		note := recreatedNote
		if m.j.ThunderbirdSync == nil && localArchiveThunderbirdSourced(m.out) {
			note += syncNowOffNote
		}
		notes = append(notes, note)
	}
	if m.j.ThunderbirdSync != nil && !presync.AttendedStartAllowed(d.cfg.goos()) {
		notes = append(notes, manualSyncStartOffNote)
	}
	resp := map[string]any{"ok": true}
	if len(notes) > 0 {
		resp["note"] = strings.Join(notes, " ")
	}
	writeJSON(w, resp)
}

// writeDescriptor is the one writer of a manual backup's record (schedule.WriteDescriptor); a
// seam, so a test can make the write fail after the scheduler step.
var writeDescriptor = schedule.WriteDescriptor

// manualTime is the time kept in a manual backup's record, so a later switch to a schedule
// starts from it: the time posted when it reads as one, else 03:00.
func manualTime(at string) string {
	probe := schedule.Spec{Name: "x", Interval: schedule.Daily, At: at, Exe: "x"}
	if at == "" || probe.Validate() != nil {
		return "03:00"
	}
	return at
}

// manualExe is the program a manual backup's record names, best effort: no scheduler runs it
// (Run now runs in MailArchive Desktop), so a program the scheduler would refuse is no reason to
// refuse the choice.
func (d *dashboard) manualExe() string {
	if d.cfg.Exe != "" {
		return d.cfg.Exe
	}
	if e, _, err := schedule.ProgramForSchedule(false); err == nil {
		return e
	}
	return ""
}

// manualAt reports that the archive at p records a backup run only on Run now (MA-949), in a
// record that is this archive's own (schedule.NameBound: never another archive's copied record).
func manualAt(p string) bool {
	dsc, err := schedule.ReadDescriptor(p)
	return err == nil && dsc.IsManual() && schedule.NameBound(p, dsc) == nil
}
