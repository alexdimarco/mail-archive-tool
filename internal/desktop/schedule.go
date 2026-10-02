package desktop

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"runtime"
	"strings"

	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/server"
)

// backupSpec builds the weekly-backup schedule Spec for the current archive. The
// scheduled task runs THIS desktop binary's headless `-capture` mode (design DC1):
// it reads the saved config + settings + sign-in token at RUN time, so a settings
// change never silently diverges from what the schedule runs, and the capture
// succeeds whether or not the dashboard app is open. Reuses internal/schedule
// unchanged (Install writes the Windows wrapper + descriptor and confirms via Query).
func (cfg Config) backupSpec() (schedule.Spec, error) {
	out := cfg.effectiveOut()
	if out == "" {
		return schedule.Spec{}, errors.New("choose an archive location first")
	}
	exe := cfg.Exe
	if exe == "" {
		if e, err := os.Executable(); err == nil {
			exe = e
		}
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

// scheduleInstall installs/updates the weekly backup (loopback-only, CSRF-guarded).
func (d *dashboard) scheduleInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, "refused", http.StatusForbidden)
		return
	}
	var in struct{ Interval, Time string }
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in)
	s := d.cfg.settings()
	if strings.TrimSpace(in.Interval) != "" {
		s.Interval = strings.ToLower(strings.TrimSpace(in.Interval))
	}
	if strings.TrimSpace(in.Time) != "" {
		s.WeeklyTime = strings.TrimSpace(in.Time)
	}
	if err := SaveSettings(d.cfg.SettingsPath, s); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	spec, err := d.cfg.backupSpec()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := schedule.Install(spec); err != nil {
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
	spec, err := d.cfg.backupSpec()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	removed, err := schedule.RemoveIfInstalled(spec)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "removed": removed})
}

// scheduleState reports the weekly-backup posture for the UI (GET; no side effect).
func (d *dashboard) scheduleState(w http.ResponseWriter, r *http.Request) {
	out := d.cfg.effectiveOut()
	resp := map[string]any{"state": "not installed", "schedulerAvailable": true}
	if out != "" {
		st := schedule.Query(schedule.DefaultNameFor(out))
		resp["state"] = st.String()
		resp["schedulerAvailable"] = st != schedule.SchedulerUnavailable
		if d, err := schedule.ReadDescriptor(out); err == nil {
			resp["interval"] = d.Interval
			resp["at"] = d.At
		}
	}
	writeJSON(w, resp)
}
