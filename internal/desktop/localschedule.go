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
	"mail-archive-tool/internal/wizard"
)

// lastRunJobArgs securely reads the archive's last-run canonical job flags for the
// INSTALL path (not display). The record lives in the archive directory and is
// UNTRUSTED (writable by anyone who can write there — design TP-D correction 2), so it
// is read with job.ReadSecure — the owner-checked, symlink-refusing, bounded read used
// for job files, which refuses a record another account owns or can change on EVERY OS
// (not util.ReadSecureFile, whose owner check is unix-only, adversarial finding A1).
// Only the Job field is taken. Absent, unreadable, odd or empty → no args.
func lastRunJobArgs(out string) ([]string, bool) {
	data, err := job.ReadSecure(filepath.Join(out, state.LastRunName), "last-run record", 64*1024)
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

// canonPath resolves p through symbolic links, falling back to a lexical clean, so two
// spellings of the same directory compare equal.
func canonPath(p string) string {
	if c, err := filepath.EvalSymlinks(p); err == nil {
		return c
	}
	return filepath.Clean(p)
}

// manifestStoreIDs is the set of the archive's own source store ids, read from the
// manifest. A reconstructed scheduled job's -input values must each canonicalize
// (state.PathSourceID — the same seed the engine recorded) to one of these
// (adversarial finding A2): the last-run record is untrusted, so the job the Desktop
// installs may only re-read the archive's OWN recorded sources, never an
// attacker-named store (an exact match also refuses an injected "/" or outside dir).
func manifestStoreIDs(out string) map[string]bool {
	ids := map[string]bool{}
	m, err := state.Load(filepath.Join(out, manifestName))
	if err != nil {
		return ids
	}
	for id := range m.Stores {
		ids[id] = true
	}
	return ids
}

// reconstructLocalJob builds the schedulable job for a LOCAL archive at out from its
// own last-run record, for the Desktop's local-schedule create (design TP-D corrections
// 1-3). The record is reconstructed through the strict whitelist mapper
// (job.FromCanonicalArgs); -out is PINNED to this archive (never the record's value);
// and every reconstructed -input must (adversarial finding A2) be a real on-disk store
// that is NOT a symbolic link or special file, does NOT resolve under <out>/_outlook-pst
// (an Outlook-app archive's deleted scratch PSTs), and IS one of the archive's own
// manifest store roots — so a tampered record can never aim the installed backup at an
// outside store. An -auto job carries no -input (it re-discovers at run time, the tool's
// own logic). ok=false → the Desktop shows no create control and points at the wizard. It
// sets no pre-sync; the caller adds the toggle.
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
	if len(j.Inputs) > 0 {
		ids := manifestStoreIDs(out)
		scratch := canonPath(filepath.Join(out, "_outlook-pst"))
		for _, in := range j.Inputs {
			fi, err := os.Lstat(in) // Lstat, NOT Stat: never follow a symlink the record names (A2)
			if err != nil || fi.Mode()&os.ModeSymlink != 0 || (!fi.IsDir() && !fi.Mode().IsRegular()) {
				return job.Job{}, false // vanished, a symlink, or a special file
			}
			if underPath(scratch, canonPath(in)) {
				return job.Job{}, false // an Outlook-app archive's scratch PSTs (correction 3)
			}
			if !ids[state.PathSourceID(in)] {
				return job.Job{}, false // not one of the archive's own recorded sources (A2)
			}
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

// scheduleInstallLocal installs (or rewrites) a LOCAL archive's run-job backup from the
// archive's own record plus the card's cadence and pre-sync toggle. It is the Desktop's
// local-schedule CREATE and EDIT (an idempotent rewrite) — never a Microsoft 365
// capture. Loopback-only + CSRF are already checked by the caller (scheduleInstall), and
// an M365/mixed archive never reaches here (the caller gates on localOnly). The run-job
// spec is the shared wizard.RunJobSpec (the MA-641 single install-path decision), and the
// kind-collision guard runs inside schedule.Install (it CheckKinds before installing).
//
// The install runs BEFORE the job file is written (adversarial finding A3): on any
// refusal nothing is on disk — no decoy job file for corroboratedRunJob to misreport,
// and CheckKind sees the true owner-side evidence rather than a file we pre-wrote. The
// cadence is persisted only once the install succeeds (finding A4).
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
	// Validate the cadence up front; refuse a non-enum value rather than silently
	// coercing it (finding A4). An empty interval keeps the default.
	iv := schedule.Weekly
	if s := strings.TrimSpace(in.Interval); s != "" {
		pv, perr := schedule.ParseInterval(s)
		if perr != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "unknown schedule interval " + s})
			return
		}
		iv = pv
	}
	at := strings.TrimSpace(in.Time)
	if at == "" {
		at = "03:00"
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
	jobPath, err := job.PathFor(j.Name)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// Install FIRST (A3): CheckKind inside schedule.Install then sees the real
	// owner-side evidence, and a refusal leaves nothing on disk.
	spec := wizard.RunJobSpec(j.Out, j.Name, jobPath, iv, at, exe)
	install := schedule.Install
	if d.cfg.InstallBackup != nil {
		install = d.cfg.InstallBackup
	}
	if err := install(spec); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// Only now write the job file the installed entry runs.
	if err := job.Write(jobPath, j); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "The scheduled backup was installed, but its settings file could not be written (" + err.Error() + "). Remove the schedule on this card and try again."})
		return
	}
	_ = saveScheduleChoice(d.cfg.SettingsPath, in.Interval, in.Time) // remember the cadence now that it is installed (A4)
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
