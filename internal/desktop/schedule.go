package desktop

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/server"
	"mail-archive-tool/internal/wizard"
)

// taskState is what the scheduler says about the weekly-backup task named for the archive location out
// (the task's name is a hash of the location it was installed for, schedule.DefaultNameFor).
func (cfg Config) taskState(out string) schedule.State {
	if strings.TrimSpace(out) == "" {
		return schedule.NotInstalled
	}
	if cfg.BackupInstalledFor != nil {
		if cfg.BackupInstalledFor(out) {
			return schedule.Installed
		}
		return schedule.NotInstalled
	}
	return schedule.Query(schedule.DefaultNameFor(out))
}

// backupTask is the archive location the installed weekly backup task is named for, and what the
// scheduler said: "" with the state when none is installed. The task is named for the location it was
// installed for and reads the archive location from the settings at run time (MA-279), so after "Save and
// keep the backup" it is named for the old location while it archives into the new one: it is looked
// for under both (Settings.BackupFor, E4-AI6).
func (cfg Config) backupTask() (string, schedule.State) {
	out := cfg.effectiveOut()
	st := cfg.taskState(out)
	if st == schedule.Installed {
		return out, st
	}
	if bf := strings.TrimSpace(cfg.settings().BackupFor); bf != "" && !samePath(bf, out) {
		if s2 := cfg.taskState(bf); s2 == schedule.Installed {
			return bf, s2
		}
	}
	return "", st
}

// backupSpec builds the weekly-backup schedule Spec for the current archive.
func (cfg Config) backupSpec() (schedule.Spec, error) { return cfg.backupSpecFor(cfg.effectiveOut()) }

// backupSpecFor builds the weekly-backup schedule Spec for the archive location out. The
// scheduled task runs THIS desktop binary's headless `-capture` mode (design DC1):
// it reads the saved config + settings + sign-in token at RUN time, so a settings
// change never silently diverges from what the schedule runs, and the capture
// succeeds whether or not the dashboard app is open. Reuses internal/schedule
// unchanged (Install writes the Windows wrapper + descriptor and confirms via Query).
func (cfg Config) backupSpecFor(out string) (schedule.Spec, error) {
	if out == "" {
		return schedule.Spec{}, errors.New("choose an archive location first")
	}
	exe := cfg.Exe
	if exe == "" {
		// §4.6.1 (MA-615): the weekly backup runs ProgramForSchedule's program (it
		// always wraps on Windows); a translocated program is refused here.
		e, _, err := schedule.ProgramForSchedule(true)
		if err != nil {
			return schedule.Spec{}, err
		}
		exe = e
	} else if err := schedule.RefuseKeptCopy(exe); err != nil {
		return schedule.Spec{}, err // the launched program is a kept copy (INT-02)
	}
	if exe == "" {
		return schedule.Spec{}, errors.New("cannot determine the program path to schedule")
	}
	s := cfg.settings()
	iv, err := schedule.ParseInterval(s.Interval)
	if err != nil {
		iv = schedule.Weekly // default to weekly
	}
	if strings.TrimSpace(s.Interval) == "" {
		iv = schedule.Weekly
	}
	at := strings.TrimSpace(s.WeeklyTime)
	if at == "" {
		at = "03:00"
	}
	name := schedule.DefaultNameFor(out)
	logPath := schedule.DefaultLogPath(out, name)
	spec := schedule.Spec{
		Name: name, Interval: iv, At: at, Exe: exe,
		Args: []string{"-capture", "-config", cfg.ConfigPath, "-out", out, "-log", logPath},
		Log:  logPath, Out: out,
		WrapperPath: schedule.DefaultWrapperPath(name),
	}
	// A wrapper (and its cmd.exe window) only for an over-long run string; the
	// common case runs direct with no console window (MA-641).
	spec.Wrapper = schedule.WrapperNeeded(spec)
	return spec, nil
}

// saveScheduleChoice remembers the interval and time chosen for the weekly backup, the
// settings step of an install, before anything is handed to the OS scheduler.
func saveScheduleChoice(path, interval, at string) error {
	return UpdateSettings(path, "schedule", func(s *Settings) {
		if strings.TrimSpace(interval) != "" {
			s.Interval = strings.ToLower(strings.TrimSpace(interval))
		}
		if strings.TrimSpace(at) != "" {
			s.WeeklyTime = strings.TrimSpace(at)
		}
	})
}

// scheduleInstall installs/updates the weekly backup (loopback-only, CSRF-guarded).
func (d *dashboard) scheduleInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, "refused", http.StatusForbidden)
		return
	}
	out := d.cfg.effectiveOut()
	// An encrypted archive is never scheduled (phase3 O5): refused before any setting
	// is written, in schedule.Install's own words (the P3-3 plan, 3b).
	if archiveKind(out) == kindEncrypted {
		writeJSON(w, map[string]any{"ok": false, "error": schedule.ErrEncryptedArchive.Error() + ": scheduled backups of an encrypted archive are not available yet; nothing was installed"})
		return
	}
	// A LOCAL Thunderbird/mbox archive (local stores, no Microsoft 365) schedules a
	// RUN-JOB that re-archives it — the Desktop's local-schedule card — reconstructed
	// from the archive's OWN record and carrying the optional pre-sync, never a
	// Microsoft 365 capture. Create and edit both come here (an idempotent rewrite);
	// repairRunJob below stays for a non-local archive's broken run-job entry.
	if d.cfg.localOnly() {
		d.scheduleInstallLocal(w, r, out)
		return
	}
	// §4.6.3/MA-617: the card's Install/Repair takes the spec from the CORROBORATED
	// kind, never the request. A corroborated run-job is REPAIRED through the
	// wizard's pure RepairSpec (from the sanitized name, -out and ProgramForSchedule,
	// never the archive-held descriptor's Job/Exe); everything else installs or
	// repairs the desktop's own capture backup below.
	if ev := schedule.BuildKindEvidence(out); ev.Has(schedule.KindRunJob) && !ev.Has(schedule.KindCapture) {
		d.repairRunJob(w, out)
		return
	}
	var in struct{ Interval, Time string }
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in)
	if err := saveScheduleChoice(d.cfg.SettingsPath, in.Interval, in.Time); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// §4.6.3/MA-617: a capture install (or repair) refuses BEFORE schedule.Install
	// when Microsoft 365 is not set up, with the same text a scheduled capture does.
	if err := d.cfg.m365Ready(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	spec, err := d.cfg.backupSpec()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	install := schedule.Install
	if d.cfg.InstallBackup != nil {
		install = d.cfg.InstallBackup
	}
	if err := install(spec); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// The task is named for the location now: the one that kept the name of an earlier location
	// (Save and keep the backup) would run beside it and archive into the same folder (E4-AI6).
	if bf := strings.TrimSpace(d.cfg.settings().BackupFor); bf != "" && !samePath(bf, spec.Out) {
		if _, err := d.removeTaskAt(bf); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "The weekly backup is installed, but the earlier one, named for " + bf + ", could not be removed: " + err.Error()})
			return
		}
	}
	_ = UpdateSettings(d.cfg.SettingsPath, "backup", func(s *Settings) { s.BackupFor = "" })
	resp := map[string]any{"ok": true}
	// §4.6.1 (MA-630): the card's Install/update result carries the one program
	// note when the schedule did not run the preferred no-console sibling (Windows
	// only; empty otherwise).
	if _, reason, perr := schedule.ProgramForSchedule(true); perr == nil {
		if note := schedule.ProgramNote(spec.Exe, reason); note != "" {
			resp["note"] = note
		}
	}
	writeJSON(w, resp)
}

// repairRunJob rebuilds a corroborated run-job schedule for out through the
// wizard's pure RepairSpec — from the sanitized name, -out and ProgramForSchedule,
// never the archive-held descriptor's Job/Exe (§4.6.3, MA-617) — or refuses with
// the scheduler entry unchanged. This is the card's Repair for a run-job: it keeps
// the schedule's kind rather than converting it to a capture.
func (d *dashboard) repairRunJob(w http.ResponseWriter, out string) {
	dsc, err := schedule.ReadDescriptor(out)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	exe, _, err := schedule.ProgramForSchedule(dsc.Wrapper != "")
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	spec, err := wizard.RepairSpec(out, dsc, exe)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	install := schedule.Install
	if d.cfg.InstallBackup != nil {
		install = d.cfg.InstallBackup
	}
	if err := install(spec); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// scheduleRemove removes the weekly backup (loopback-only, CSRF-guarded).
func (d *dashboard) scheduleRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, "refused", http.StatusForbidden)
		return
	}
	if _, err := d.cfg.backupSpec(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// §4.6.3/MA-617: a run-job's Remove also deletes the archive's job file, so the
	// schedule is not left a dead record pointing at a job that no longer runs. Read
	// the corroborated job path before the entry goes (the job file is owner-side,
	// untouched by removing the scheduler entry).
	runJobFile := ""
	if ev := schedule.BuildKindEvidence(d.cfg.effectiveOut()); ev.Has(schedule.KindRunJob) {
		runJobFile = ev.JobPath
	}
	removed, err := d.removeTasks()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if runJobFile != "" {
		_ = os.Remove(runJobFile)
	}
	resp := map[string]any{"ok": true, "removed": len(removed) > 0}
	// A descriptor the tool cannot read is never deleted (rulings §13.6, ESC-10):
	// name it with the remedy, as `schedule -remove` does.
	for _, p := range removed {
		if left := schedule.UnreadableDescriptor(p); left != "" {
			resp["left"] = left
			resp["remedy"] = schedule.UnreadableDescriptorRemedy
		}
	}
	writeJSON(w, resp)
}

// removeBackup removes the weekly backup, wherever its task is named for (the archive the settings
// point at now, or the one it was installed for): what "Save and remove the weekly backup" does before it
// saves the new location (XC22).
func (d *dashboard) removeBackup() error {
	if d.cfg.RemoveBackup != nil {
		return d.cfg.RemoveBackup()
	}
	_, err := d.removeTasks()
	return err
}

// removeTasks removes the weekly backup task named for the archive location now, and the one named for
// the location it was installed for when Save and keep the backup left them apart (E4-AI6). It
// returns the archive locations whose task was removed.
func (d *dashboard) removeTasks() (removed []string, err error) {
	out := d.cfg.effectiveOut()
	paths := []string{out}
	if bf := strings.TrimSpace(d.cfg.settings().BackupFor); bf != "" && !samePath(bf, out) {
		paths = append(paths, bf)
	}
	for _, p := range paths {
		if p == "" {
			continue
		}
		r, err := d.removeTaskAt(p)
		if err != nil {
			return removed, err
		}
		if r {
			removed = append(removed, p)
		}
	}
	if len(removed) > 0 {
		_ = UpdateSettings(d.cfg.SettingsPath, "backup", func(s *Settings) { s.BackupFor = "" })
	}
	return removed, nil
}

// removeTaskAt removes the weekly backup task named for the archive location p, if there is one.
func (d *dashboard) removeTaskAt(p string) (bool, error) {
	if d.cfg.RemoveBackupFor != nil {
		if d.cfg.taskState(p) != schedule.Installed {
			return false, nil
		}
		return true, d.cfg.RemoveBackupFor(p)
	}
	spec, err := d.cfg.backupSpecFor(p)
	if err != nil {
		return false, err
	}
	return schedule.RemoveIfInstalled(spec)
}

// runBackup runs this archive's recorded run-job backup NOW, in-process (the manual twin of
// the scheduled run), streaming progress through the shared activity state (/api/activity).
// Loopback-only, CSRF-guarded. Only a corroborated run-job backup runs here — a Microsoft 365
// archive uses "Archive now".
func (d *dashboard) runBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, "refused: same-origin, tokened, local requests only", http.StatusForbidden)
		return
	}
	out := d.cfg.effectiveOut()
	ev := schedule.BuildKindEvidence(out)
	if !ev.Has(schedule.KindRunJob) || ev.JobPath == "" {
		writeJSON(w, map[string]any{"ok": false, "error": "this archive has no run-job backup to run here"})
		return
	}
	jobPath := ev.JobPath
	if archiveKind(out) == kindEncrypted {
		d.runBackupKeyed(w, out, jobPath)
		return
	}
	if err := d.runJob(func(logger *log.Logger) (string, error) {
		return wizard.RunJobInProcess(jobPath, logger)
	}); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// runBackupKeyed is Run now for an encrypted archive (design-encrypted-archives-
// phase3 §3.2, §3.6, §6; the P3-3 plan, 3d): only while it is unlocked here, on a
// write lease of its own, taken as Run now is clicked — so a Lock, an idle lock or a
// switch after it does not stop the run — and destroyed when the run ends, or at
// once if the run cannot start; healed first, a failed heal only a warning, a fork
// never healed. Shut down cancels it.
// runLease takes Run now's write lease; a seam, so a test can see it destroyed.
var runLease = func(e *keyEntry, o keyring.OpenOptions) (*keyring.Unlocked, error) { return e.u.LeaseForWrite(o) }

func (d *dashboard) runBackupKeyed(w http.ResponseWriter, out, jobPath string) {
	e := d.cfg.Keys.get(out)
	if e == nil {
		e = d.cfg.Keys.remembered(out) // a remembered key opens it, unless the person locked it (O3; FID-03)
	}
	if e == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "This encrypted archive is locked: unlock it on its Encryption card, then click Run now again."})
		return
	}
	lease, err := runLease(e, keyring.OpenOptions{Store: d.cfg.Store})
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := d.runKeyedJobOn(out, func(j keyedJob, logger *log.Logger) (string, error) {
		defer lease.Destroy()
		if lease.NeedsHeal() && !lease.Forked() {
			if herr := keyring.Heal(lease); herr != nil {
				logger.Printf("warning: one copy of this archive's key file is damaged and could not be rewritten: %v", herr)
			} else {
				logger.Printf("The damaged copy of this archive's key file was rewritten from the good one.")
			}
		}
		return wizard.RunJobInProcessKeyed(j.ctx, jobPath, logger, func(o *app.Options) (func(), error) {
			o.ProgramSkip, o.OnProgramWait = j.skip, j.onWait
			return keyring.Apply(o, lease)
		})
	}); err != nil {
		lease.Destroy() // the run never started: its key goes now
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// scheduleState reports the weekly-backup posture for the UI (GET; no side effect).
func (d *dashboard) scheduleState(w http.ResponseWriter, r *http.Request) {
	out := d.cfg.effectiveOut()
	resp := map[string]any{"state": "not installed", "schedulerAvailable": true}
	if out != "" {
		path, st := d.cfg.backupTask()
		resp["state"] = st.String()
		resp["schedulerAvailable"] = st != schedule.SchedulerUnavailable
		if path == "" {
			path = out
		}
		if d, err := schedule.ReadDescriptor(path); err == nil {
			resp["interval"] = d.Interval
			resp["at"] = d.At
		}
	}
	writeJSON(w, resp)
}
