package desktop

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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

// scheduleInstallLocal installs or edits a LOCAL archive's run-job backup — never a
// Microsoft 365 capture. Loopback-only + CSRF are already checked by the caller
// (scheduleInstall), and an M365/mixed archive never reaches here (the caller gates on
// localOnly). The run-job spec is the shared wizard.RunJobSpec (the MA-641 single
// install-path decision), and the kind-collision guard runs inside schedule.Install (it
// CheckKinds before installing).
//
// An edit changes only what was changed (design-desktop-settings-card 3.5, C-4): with a
// job file that reads securely it is a read-modify-write of that file through job.Read
// and job.Write, changing only thunderbird_sync, and the cadence goes to the scheduler
// entry and descriptor only. The job rebuilt from the archive's own last-run record is
// written on a create only (an edit still needs the record to rebuild; see below). A job file that exists but cannot be read is never overwritten (C-6), and
// a sync request that cannot be honoured is refused, never dropped (C-10): every refusal
// comes before anything is written.
//
// The install runs BEFORE the job file is written (adversarial finding A3): on any
// refusal nothing is on disk — no decoy job file for corroboratedRunJob to misreport,
// and CheckKind sees the true owner-side evidence rather than a file we pre-wrote. The
// settings file's interval and time are the capture's record and are not written here.
func (d *dashboard) scheduleInstallLocal(w http.ResponseWriter, r *http.Request, out string) {
	var in struct {
		Interval string `json:"interval"`
		Time     string `json:"time"`
		// Presync is the sync box as posted; absent (a box the page did not show, or the
		// card's Repair) means unchanged, never off (C-10).
		Presync *bool `json:"presync"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in)

	name := schedule.DefaultNameFor(out)
	jobPath, err := job.PathFor(name)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// The job file this install would overwrite (C-6): read securely, or refused with
	// nothing written when it is there and cannot be read.
	cur, exists, reason := readJobToEdit(jobPath, out)
	if reason != "" {
		writeJSON(w, map[string]any{"ok": false, "error": unreadableJobText(jobPath, reason)})
		return
	}

	// A Repair posts an empty body (the Repair button sends no interval/time/presync,
	// because those inputs are not rendered on a scheduled local card — MA-691). That must
	// re-point/fix the schedule WITHOUT changing its settings (friction HIGH-3): so when no
	// interval is posted, keep the installed cadence (from the current descriptor); the
	// pre-sync is unchanged because none is posted.
	if strings.TrimSpace(in.Interval) == "" {
		if desc, derr := schedule.ReadDescriptor(out); derr == nil {
			in.Interval, in.Time = desc.Interval, desc.At
		}
	}

	syncOn := exists && cur.ThunderbirdSync != nil
	wantSync := syncOn
	if in.Presync != nil {
		wantSync = *in.Presync
	}
	// Switching the sync on is refused, not dropped, when it cannot be honoured here (C-10).
	if wantSync && !syncOn {
		if refusal := d.syncOnRefusal(out); refusal != "" {
			writeJSON(w, map[string]any{"ok": false, "error": refusal})
			return
		}
	}

	// The archive's own record must still rebuild, on an edit too: TestScheduleInstallLocal
	// (MA-581) pins that refusal for an archive whose record is gone while its job file is
	// there. Design 3.3 (R3C-16, MA-857) keys an edit to the job file alone; dropping this
	// precondition waits on the owner accepting that test's amendment. The rebuilt job is
	// used on a create only (C-4): an edit keeps the job file as it is.
	rebuilt, ok := reconstructLocalJob(out)
	if !ok {
		writeJSON(w, map[string]any{"ok": false, "error": "MailArchive Desktop can’t tell how this archive was built, so it can’t schedule a backup for it from here. Open the MailArchive wizard (mailarchive wizard) to set one up."})
		return
	}
	j := cur // an edit: the job file as it is
	if !exists {
		j = rebuilt // a create: rebuilt from the archive's own record
		j.Name = name
	}
	syncChanged := wantSync != syncOn
	if syncChanged {
		if wantSync {
			j.ThunderbirdSync = &job.ThunderbirdSync{}
		} else {
			j.ThunderbirdSync = nil
		}
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
	} else if kerr := schedule.RefuseKeptCopy(exe); kerr != nil {
		writeJSON(w, map[string]any{"ok": false, "error": kerr.Error()}) // a kept copy (INT-02)
		return
	}
	// Install FIRST (A3): CheckKind inside schedule.Install then sees the real
	// owner-side evidence, and a refusal leaves nothing on disk.
	spec := wizard.RunJobSpec(out, name, jobPath, iv, at, exe)
	install := schedule.Install
	if d.cfg.InstallBackup != nil {
		install = d.cfg.InstallBackup
	}
	if err := install(spec); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// Only now write the job file the installed entry runs: on a create, or when the sync
	// changed. An edit of the cadence alone leaves the job file as it is.
	if !exists || syncChanged {
		if err := job.Write(jobPath, j); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "The scheduled backup was installed, but its settings file could not be written (" + err.Error() + "). Remove the schedule on this card and try again."})
			return
		}
	}
	writeJSON(w, map[string]any{"ok": true})
}

// readJobToEdit reads the job file at path that an install for the archive out would
// overwrite (C-6). A missing file is no job (a create). A file that is there and reads
// securely through job.Read, naming this archive, is the job to edit. Anything else is a
// file that must not be overwritten: reason says why, in plain words.
func readJobToEdit(path, out string) (j job.Job, exists bool, reason string) {
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return job.Job{}, false, ""
		}
		return job.Job{}, false, "it can't be looked at: " + err.Error()
	}
	j, err := job.Read(path)
	if err != nil {
		return job.Job{}, false, jobUnreadableReason(path)
	}
	if !schedule.SameArchivePath(j.Out, out, runtime.GOOS) {
		return job.Job{}, false, "it backs up another archive folder, " + j.Out
	}
	return j, true, ""
}

// jobUnreadableReason says in plain words why the job file at path, which job.Read
// refused, can't be read.
func jobUnreadableReason(path string) string {
	fi, err := os.Lstat(path)
	switch {
	case err != nil:
		return "it can't be looked at: " + err.Error()
	case fi.Mode()&os.ModeSymlink != 0:
		return "it is a link to another file, which MailArchive does not follow"
	case !fi.Mode().IsRegular():
		return "it is not an ordinary file"
	case fi.Size() > 64*1024:
		return "it is far larger than a backup settings file"
	}
	if data, rerr := os.ReadFile(path); rerr == nil {
		var v struct {
			Version int    `json:"version"`
			Out     string `json:"out"`
		}
		switch {
		case json.Unmarshal(data, &v) != nil:
			return "it is damaged: it is not a settings file MailArchive can read"
		case v.Version > job.Version:
			return "a newer version of MailArchive wrote it"
		case strings.TrimSpace(v.Out) == "":
			return "it names no archive folder"
		}
	}
	return "another account owns it or can change it"
}

// unreadableJobText is the refusal for a job file that can't be read (section 6, C-6).
func unreadableJobText(path, reason string) string {
	return "This archive's backup settings file " + path + " can't be read (" + reason + "). Re-create the backup in the MailArchive wizard, or update MailArchive if a newer version made it. Nothing was changed."
}

// syncOnRefusal is why the Thunderbird sync can't be switched on for the local archive at
// out, in section 6's words, or "" when it can: it runs only on macOS and Windows, only for
// an archive made from a Thunderbird profile, and only while that profile is on this
// computer (C-10).
func (d *dashboard) syncOnRefusal(out string) string {
	if goos := d.cfg.goos(); goos != "darwin" && goos != "windows" {
		return "Thunderbird sync works on macOS and Windows only, so nothing was changed."
	}
	if localArchiveThunderbirdSourced(out) {
		return ""
	}
	m, err := state.Load(filepath.Join(out, manifestName))
	if err != nil {
		return "Thunderbird sync can't be switched on: MailArchive can't read this archive's record right now. Nothing was changed."
	}
	for id := range m.Stores {
		if !strings.HasPrefix(id, "path:") {
			continue
		}
		if p := strings.TrimPrefix(id, "path:"); !pathPresent(p) {
			return "Thunderbird sync can't be switched on: the mail folder this archive was made from, " + p + ", isn't on this computer right now. Nothing was changed."
		}
	}
	return "This archive wasn't made from a Thunderbird profile on this computer, so there is no Thunderbird to sync. Nothing was changed."
}

// pathPresent reports whether something is at p (a link counts; it is not followed).
func pathPresent(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
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
