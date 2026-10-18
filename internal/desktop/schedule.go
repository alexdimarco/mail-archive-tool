package desktop

import (
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"strings"

	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/server"
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
	return schedule.Spec{
		Name: name, Interval: iv, At: at, Exe: exe,
		Args: []string{"-capture", "-config", cfg.ConfigPath, "-out", out, "-log", logPath},
		Log:  logPath, Out: out,
		Wrapper: runtime.GOOS == "windows", WrapperPath: schedule.DefaultWrapperPath(name),
	}, nil
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
	var in struct{ Interval, Time string }
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in)
	if err := saveScheduleChoice(d.cfg.SettingsPath, in.Interval, in.Time); err != nil {
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
	removed, err := d.removeTasks()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
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
