package desktop

import (
	"encoding/json"
	"errors"
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
			// A network or non-local path is not checked at all (C-17, MA-869): no file check
			// reaches another host, and such a backup is not rebuilt from here.
			if isRemotePath(in) {
				return job.Job{}, false
			}
			fi, err := lstatSource(in) // Lstat, NOT Stat: never follow a symlink the record names (A2)
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

// lstatSource is the one look at a mail source's storage when a local backup is rebuilt; a
// seam, so a test can see which paths are looked at and model a stalled store.
var lstatSource = os.Lstat

// underPath reports whether p is dir itself or a path inside it (clean, separator-
// aware, so "/a/bc" is not treated as under "/a/b").
func underPath(dir, p string) bool {
	dir, p = filepath.Clean(dir), filepath.Clean(p)
	return p == dir || strings.HasPrefix(p, dir+string(os.PathSeparator))
}

// scheduleInstallLocal installs or edits an archive's run-job backup — never a Microsoft
// 365 capture. Loopback-only + CSRF are already checked by the caller (scheduleInstall),
// which routes here by section 3.2's table: a local archive with no capture counted, a
// corroborated run-job (a mixed archive's included), or a broken run-job's Repair; never
// where a capture is counted (section 4). The run-job spec is the shared wizard.RunJobSpec (the MA-641 single
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
//
// A failure after the scheduler step is answered as it happened (section 9, R3C-13): the
// scheduler is asked first whether an entry already existed under the name, and a rollback
// runs only when none did, so a create running as a Repair over an old entry never removes it.
// recreate is a broken run-job's Repair (its job file missing): its reply says the file was made
// again and that the sync is off (section 5, C-9).
func (d *dashboard) scheduleInstallLocal(w http.ResponseWriter, in scheduleRequest, out string, recreate bool) {
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
	existed := d.cfg.taskState(out) == schedule.Installed // asked before step 2 (R3C-13)
	if err := d.installer()(spec); err != nil {
		if errors.Is(err, schedule.ErrNotRecorded) {
			writeJSON(w, map[string]any{"ok": false, "error": d.notRecordedReply(out, exists, existed, exe, err)})
			return
		}
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// Only now write the job file the installed entry runs: on a create, or when the sync
	// changed. An edit of the cadence alone leaves the job file as it is.
	if !exists || syncChanged {
		if err := writeJob(jobPath, j); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": d.jobWriteFailedReply(out, exists, existed, iv, at, err)})
			return
		}
	}
	if !existed {
		d.cfg.clearAlertStreak(out) // a create starts with no failing streak (R3C-10)
	}
	resp := map[string]any{"ok": true}
	var notes []string
	if recreate && !exists {
		note := recreatedNote
		if j.ThunderbirdSync == nil && localArchiveThunderbirdSourced(out) {
			note += syncNowOffNote
		}
		notes = append(notes, note)
	}
	if iv == schedule.Hourly {
		notes = append(notes, d.cfg.hourlyNotes(out)...) // what 24 runs a day costs (3.7)
	}
	if len(notes) > 0 {
		resp["note"] = strings.Join(notes, " ")
	}
	writeJSON(w, resp)
}

// writeJob is the one writer of a job file (job.Write); a seam, so a test can make the write
// fail after the scheduler step (section 9, step 3).
var writeJob = job.Write

// notRecordedReply undoes or restores after Install took the entry but could not record it in
// the archive (step 2b, section 9), and says what is now true. An edit puts the previous entry
// back from the descriptor, which still holds the old cadence; a create removes the entry it
// made, only when none existed before.
func (d *dashboard) notRecordedReply(out string, edit, existed bool, exe string, err error) string {
	reason := notRecordedReason(err)
	if edit {
		if dsc, derr := schedule.ReadDescriptor(out); derr == nil {
			spec, serr := wizard.RepairSpec(out, dsc, exe)
			if serr == nil {
				serr = d.installer()(spec)
			}
			// Put back, entry and record agree again: a restore that is itself not recorded
			// leaves the old entry beside the old descriptor, which is what was there.
			if serr == nil || errors.Is(serr, schedule.ErrNotRecorded) {
				return "The backup schedule was not changed: it could not be recorded in the archive (" + reason + ")."
			}
			return "The new schedule is running, but it could not be recorded in the archive (" + reason + "), and the earlier schedule could not be put back (" + serr.Error() + "). Reload this page and click Repair, or save the schedule it shows: either puts the recorded schedule back."
		}
		if existed { // nothing recorded to put back from
			return "The new schedule is running, but it could not be recorded in the archive (" + reason + ")."
		}
	}
	if existed {
		return "The backup could not be recorded in the archive (" + reason + "), so it was not set up again; the scheduler entry that was there is kept."
	}
	if rerr := d.undoInstall(out, false); rerr != nil {
		return "The backup could not be recorded in the archive (" + reason + "), and the scheduler entry it made could not be taken out again (" + rerr.Error() + "): click Remove on this card to take it out."
	}
	return "The backup could not be recorded in the archive (" + reason + "), so nothing was scheduled."
}

// jobWriteFailedReply answers a job file that could not be written after the scheduler step
// (step 3, section 9). The old job file is still whole (job.Write renames a finished copy over
// it), so an edit keeps the new cadence and the old sync; a create removes what it installed,
// only when no entry existed before. No reply advises removing a backup that is running.
func (d *dashboard) jobWriteFailedReply(out string, edit, existed bool, iv schedule.Interval, at string, err error) string {
	if edit {
		return "The new schedule (" + cadenceWords(string(iv), at) + ") is in effect; Thunderbird sync was not changed: " + err.Error() + "."
	}
	if existed {
		return "The backup's settings file could not be written (" + err.Error() + "), so the backup was not set up again; the scheduler entry that was there is kept."
	}
	if rerr := d.undoInstall(out, true); rerr != nil {
		return "The backup could not be set up (" + err.Error() + "), and what was installed could not be taken out again (" + rerr.Error() + "): click Remove on this card to take it out."
	}
	return "The backup could not be set up (" + err.Error() + "), so nothing was scheduled."
}

// notRecordedReason is the descriptor write's own error inside Install's step-2b error.
func notRecordedReason(err error) string {
	var nr *schedule.NotRecordedError
	if errors.As(err, &nr) && nr.Cause != nil {
		return nr.Cause.Error()
	}
	return err.Error()
}

// undoInstall takes out the entry a create just installed under this archive's name: the entry
// alone after a step-2b failure (nothing was recorded), the entry and its descriptor after a
// job-file write failure (step 3).
func (d *dashboard) undoInstall(out string, withDescriptor bool) error {
	if d.cfg.RemoveBackupFor != nil {
		return d.cfg.RemoveBackupFor(out)
	}
	name := schedule.DefaultNameFor(out)
	s := schedule.Spec{Name: name, Out: out, WrapperPath: schedule.DefaultWrapperPath(name)}
	if withDescriptor {
		return schedule.Remove(s)
	}
	return schedule.RemoveLiveEntry(s)
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
		if p := strings.TrimPrefix(id, "path:"); !isRemotePath(p) && !pathPresent(p) {
			return "Thunderbird sync can't be switched on: the mail folder this archive was made from, " + p + ", isn't on this computer right now. Nothing was changed."
		}
	}
	return "This archive wasn't made from a Thunderbird profile on this computer, so there is no Thunderbird to sync. Nothing was changed."
}

// pathPresent reports whether something is at p (a link counts; it is not followed).
func pathPresent(p string) bool {
	_, err := lstatSource(p)
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
