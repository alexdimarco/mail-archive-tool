package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/job"
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
	if bf := cfg.captureBackupFor(); bf != "" && !samePath(bf, out) {
		if s2 := cfg.taskState(bf); s2 == schedule.Installed {
			return bf, s2
		}
	}
	return "", st
}

// namesRunJob reports whether the archive folder is a run-job's (design-desktop-settings-card
// section 8): the job file named for it, job.PathFor(DefaultNameFor(folder)), reads securely
// through job.Read and its Out is the same archive as folder. Owner-side evidence only; nothing
// inside the archive is read. A job file that cannot be read fails the test (section 8's residual).
func namesRunJob(folder string) bool {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return false
	}
	p, err := job.PathFor(schedule.DefaultNameFor(folder))
	if err != nil {
		return false
	}
	j, err := job.Read(p)
	return err == nil && schedule.SameArchivePath(j.Out, folder, runtime.GOOS)
}

// captureBackupFor is Settings.BackupFor when it can belong to a capture, "" otherwise. BackupFor is
// kept only for a capture (section 8): one an earlier version set for a run-job's folder would let
// Remove, or a capture install, on another archive delete that run-job's scheduler entry, and would
// show that run-job as this archive's backup. The readers skip it; it writes nothing.
func (cfg Config) captureBackupFor() string {
	bf := strings.TrimSpace(cfg.settings().BackupFor)
	if bf == "" || namesRunJob(bf) {
		return ""
	}
	return bf
}

// dropRunJobBackupFor is captureBackupFor for the two write paths (Remove and the capture install's
// clean-up): a BackupFor that names a run-job is cleared in the settings file before they act, and
// nothing else is written.
func (d *dashboard) dropRunJobBackupFor() string {
	bf := strings.TrimSpace(d.cfg.settings().BackupFor)
	if bf != "" && namesRunJob(bf) {
		_ = clearBackupForIf(d.cfg.SettingsPath, bf)
		return ""
	}
	return bf
}

// clearBackupForIf clears Settings.BackupFor when it is still bf, and changes nothing else.
func clearBackupForIf(settingsPath, bf string) error {
	return UpdateSettings(settingsPath, "backupFor", func(s *Settings) {
		if strings.TrimSpace(s.BackupFor) == bf {
			s.BackupFor = ""
		}
	})
}

// ClearRunJobBackupFor is the Desktop's one-time start-up clean-up (design-desktop-settings-card
// section 8, rev4.1): a BackupFor an earlier version recorded for a run-job's folder is cleared in the
// settings file at settingsPath. It writes nothing else — no scheduler entry, descriptor or job file
// is touched — and nothing at all when BackupFor is unset, belongs to a capture, or names a job file
// that cannot be read. It reports whether it cleared one.
func ClearRunJobBackupFor(settingsPath string) (bool, error) {
	bf := strings.TrimSpace(LoadSettings(settingsPath).BackupFor)
	if bf == "" || !namesRunJob(bf) {
		return false, nil
	}
	if err := clearBackupForIf(settingsPath, bf); err != nil {
		return false, err
	}
	return true, nil
}

// backupSpec builds the weekly-backup schedule Spec for the current archive with the cadence the
// settings file records as the capture's last install. No install or repair builds from it (design
// section 3.6 step 4): they use backupSpecFor with the posted, or the descriptor's, cadence.
func (cfg Config) backupSpec() (schedule.Spec, error) {
	s := cfg.settings()
	iv, err := schedule.ParseInterval(s.Interval)
	if err != nil || strings.TrimSpace(s.Interval) == "" {
		iv = schedule.Weekly // default to weekly
	}
	return cfg.backupSpecFor(cfg.effectiveOut(), iv, s.WeeklyTime)
}

// backupSpecFor builds the weekly-backup schedule Spec for the archive location out, at the cadence
// iv and the time at ("" is 03:00). The
// scheduled task runs THIS desktop binary's headless `-capture` mode (design DC1):
// it reads the saved config + settings + sign-in token at RUN time, so a settings
// change never silently diverges from what the schedule runs, and the capture
// succeeds whether or not the dashboard app is open. Reuses internal/schedule
// unchanged (Install writes the Windows wrapper + descriptor and confirms via Query).
func (cfg Config) backupSpecFor(out string, iv schedule.Interval, at string) (schedule.Spec, error) {
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
	at = strings.TrimSpace(at)
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
		http.Error(w, csrfRefused, http.StatusForbidden)
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
	// §4.6.3/MA-617: a capture install (or repair) refuses BEFORE schedule.Install
	// when Microsoft 365 is not set up, with the same text a scheduled capture does.
	if err := d.cfg.m365Ready(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// The capture is built from the values it is given, never from the settings file
	// (design-desktop-settings-card 3.6 step 4, C-13, R3C-14): an install from the posted
	// cadence; an empty-body Repair from the installed descriptor's, so it keeps its own
	// cadence even when the one settings file holds another archive's.
	iv, at, err := d.captureCadence(in.Interval, in.Time)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	spec, err := d.cfg.backupSpecFor(out, iv, at)
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
	// Recorded only now that it is installed, and before the clean-up below, which can
	// return early (C-13, R3C-14): the settings file is the capture's record of what it
	// last installed, never an input to an install.
	if err := saveScheduleChoice(d.cfg.SettingsPath, string(spec.Interval), spec.At); err != nil {
		log.Printf("the scheduled backup is installed, but the settings file could not record its cadence: %v", err)
	}
	// The task is named for the location now: the one that kept the name of an earlier location
	// (Save and keep the backup) would run beside it and archive into the same folder (E4-AI6).
	// A BackupFor that names a run-job's folder is not this capture's: it is cleared, never removed
	// (design-desktop-settings-card section 8).
	if bf := d.dropRunJobBackupFor(); bf != "" && !samePath(bf, spec.Out) {
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

// captureCadence is the cadence a capture install or repair builds from (3.6 step 4): the posted
// interval and time; with none posted (the card's Repair), the installed descriptor's — the one
// recorded for the folder the capture is named for, which may be the earlier location's (BackupFor);
// with no descriptor either, weekly at 03:00. A posted interval MailArchive does not know is refused
// before anything is written, never turned into another cadence.
func (d *dashboard) captureCadence(interval, at string) (schedule.Interval, string, error) {
	if strings.TrimSpace(interval) == "" {
		p, _ := d.cfg.backupTask()
		if p == "" {
			p = d.cfg.effectiveOut()
		}
		if dsc, err := schedule.ReadDescriptor(p); err == nil && strings.TrimSpace(dsc.Interval) != "" {
			interval, at = dsc.Interval, dsc.At
		} else {
			return schedule.Weekly, "03:00", nil
		}
	}
	iv, err := schedule.ParseInterval(interval)
	if err != nil {
		return "", "", fmt.Errorf("%q is not a backup schedule MailArchive knows (choose Daily or Weekly). Nothing was changed.", strings.TrimSpace(interval))
	}
	return iv, strings.TrimSpace(at), nil
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
		http.Error(w, csrfRefused, http.StatusForbidden)
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
	// A BackupFor that names a run-job's folder is not this archive's backup: it is cleared, and
	// that run-job's entry is left alone (design-desktop-settings-card section 8).
	if bf := d.dropRunJobBackupFor(); bf != "" && !samePath(bf, out) {
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
	spec, err := d.cfg.backupSpecFor(p, schedule.Weekly, "") // only its name and folder matter to a removal
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
		http.Error(w, csrfRefused, http.StatusForbidden)
		return
	}
	out := d.cfg.effectiveOut()
	ev := schedule.BuildKindEvidence(out)
	if !ev.Has(schedule.KindRunJob) || ev.JobPath == "" {
		writeJSON(w, map[string]any{"ok": false, "error": noRunNowText(d.cfg.goos(), out)})
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
		res, err := withProgramLine(logger, func(logger *log.Logger) (string, error) {
			return wizard.RunJobInProcessKeyed(j.ctx, jobPath, logger, func(o *app.Options) (func(), error) {
				o.ProgramSkip, o.OnProgramWait, o.ProgramDownload = j.skip, j.onWait, d.life.Context()
				return keyring.Apply(o, lease)
			})
		})
		return runNowResultHere(res), err
	}); err != nil {
		lease.Destroy() // the run never started: its key goes now
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// noRunNowText is Run now's refusal for an archive with no backup set up on this
// computer for it to repeat, in plain words with what to do instead: an encrypted
// archive made at the command line is backed up there, by the exact command (P3-8, B26).
func noRunNowText(goos, out string) string {
	if archiveKind(out) == kindEncrypted {
		return "Run now has nothing to run: no backup was set up on this computer for this encrypted archive. " + encBackupHow(goos, out, false)
	}
	return "Run now has nothing to run: no backup was set up on this computer for this archive. A Microsoft 365 archive is brought up to date with Archive now; an archive of local mail files with its scheduled backup, set up on this card or in the MailArchive wizard (mailarchive wizard)."
}

// wizardReadThem is how the wizard's keyed run ends its result, for someone outside
// MailArchive Desktop; runNowResultHere says it to someone already in it (P3-8, Tier C).
const wizardReadThem = "; open it in MailArchive Desktop to read them."

// runNowResultHere is a keyed Run now's result as Desktop shows it: the messages are read
// here, with Open archive, while the archive is unlocked.
func runNowResultHere(res string) string {
	return strings.Replace(res, wizardReadThem, "; read them here with Open archive while it is unlocked.", 1)
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
