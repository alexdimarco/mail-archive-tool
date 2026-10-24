package desktop

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/wizard"
)

// lastRunJobArgs securely reads the archive's last-run canonical job flags for the
// INSTALL path (not display). The record lives in the archive directory and is
// UNTRUSTED (writable by anyone who can write there — design TP-D correction 2), so
// it is read with util.ReadSecureFile — refusing a symlink, a non-regular file or an
// oversized one — rather than state.ReadLastRun's plain os.ReadFile, and only the Job
// field is taken. Absent, unreadable, odd or empty → no args.
func lastRunJobArgs(out string) ([]string, bool) {
	data, err := util.ReadSecureFile(filepath.Join(out, state.LastRunName), "last-run record", 64*1024)
	if err != nil {
		return nil, false
	}
	var lr struct {
		Job []string `json:"job"`
	}
	if json.Unmarshal(data, &lr) != nil || len(lr.Job) == 0 {
		return nil, false
	}
	return lr.Job, true
}

// reconstructLocalJob builds the schedulable job for a LOCAL archive at out from its
// own last-run record, for the Desktop's local-schedule create (design TP-D
// corrections 1-3): the record is reconstructed through the strict whitelist mapper
// (job.FromCanonicalArgs), -out is PINNED to this archive (never the record's value),
// and the job is refused unless every reconstructed -input still EXISTS and none lives
// under <out>/_outlook-pst (an Outlook-app archive's deleted scratch PSTs, which must
// not be scheduled). ok=false → the Desktop shows no create control and points at the
// wizard. It sets no pre-sync; the caller adds the toggle.
func reconstructLocalJob(out string) (job.Job, bool) {
	args, ok := lastRunJobArgs(out)
	if !ok {
		return job.Job{}, false
	}
	j, ok := job.FromCanonicalArgs(args)
	if !ok {
		return job.Job{}, false
	}
	j.Out = out // correction 2b: pin to the archive shown, never the record's -out
	scratch := filepath.Join(out, "_outlook-pst")
	for _, in := range j.Inputs {
		if underPath(scratch, in) {
			return job.Job{}, false // an Outlook-app archive's scratch PSTs (correction 3)
		}
		if _, err := os.Stat(in); err != nil {
			return job.Job{}, false // a vanished input — never schedule a broken job
		}
	}
	return j, true
}

// underPath reports whether p is dir itself or a path inside it (clean, separator-
// aware, so "/a/bc" is not treated as under "/a/b").
func underPath(dir, p string) bool {
	dir, p = filepath.Clean(dir), filepath.Clean(p)
	return p == dir || strings.HasPrefix(p, dir+string(os.PathSeparator))
}

// scheduleInstallLocal installs (or rewrites) a LOCAL archive's run-job backup from
// the archive's own record plus the card's cadence and pre-sync toggle. It is the
// Desktop's local-schedule CREATE and EDIT (an idempotent rewrite) — never a
// Microsoft 365 capture. Loopback-only + CSRF are already checked by the caller
// (scheduleInstall), and an M365/mixed archive never reaches here (the caller gates
// on localOnly). The run-job spec is the shared wizard.RunJobSpec (the MA-641 single
// install-path decision), and the kind-collision guard runs inside schedule.Install
// (it CheckKinds before installing). Nothing is written or installed on a refusal.
func (d *dashboard) scheduleInstallLocal(w http.ResponseWriter, r *http.Request, out string) {
	var in struct {
		Interval string `json:"interval"`
		Time     string `json:"time"`
		Presync  bool   `json:"presync"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in)

	j, ok := reconstructLocalJob(out)
	if !ok {
		writeJSON(w, map[string]any{"ok": false, "error": "MailArchive Desktop can’t tell how this archive was built, so it can’t schedule a backup for it from here. Open the MailArchive wizard (mailarchive wizard) to set one up."})
		return
	}
	j.Name = schedule.DefaultNameFor(out)
	// The pre-sync is offered only on an adapter platform for a Thunderbird-sourced
	// archive (MA-582); honour the toggle only then (default off, R30).
	if goos := d.cfg.goos(); in.Presync && (goos == "darwin" || goos == "windows") && localArchiveThunderbirdSourced(out) {
		j.ThunderbirdSync = &job.ThunderbirdSync{}
	}
	if err := saveScheduleChoice(d.cfg.SettingsPath, in.Interval, in.Time); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	exe := d.cfg.Exe
	if exe == "" {
		e, _, perr := schedule.ProgramForSchedule(false)
		if perr != nil {
			writeJSON(w, map[string]any{"ok": false, "error": perr.Error()})
			return
		}
		exe = e
	}
	iv, err := schedule.ParseInterval(in.Interval)
	if err != nil || strings.TrimSpace(in.Interval) == "" {
		iv = schedule.Weekly
	}
	at := strings.TrimSpace(in.Time)
	if at == "" {
		at = "03:00"
	}
	jobPath, err := job.PathFor(j.Name)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := job.Write(jobPath, j); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	spec := wizard.RunJobSpec(j.Out, j.Name, jobPath, iv, at, exe)
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

// currentJobSyncsThunderbird reports whether the archive's installed run-job already
// carries the pre-sync, so the Desktop's toggle shows CHECKED on load (edit prefill).
// It reads the job file through internal/job (the secure read); a missing/unreadable
// job is false.
func currentJobSyncsThunderbird(out string) bool {
	p, err := job.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		return false
	}
	j, err := job.Read(p)
	if err != nil {
		return false
	}
	return j.ThunderbirdSync != nil
}
