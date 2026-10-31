package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// The archive's kind for its backup options (design-desktop-settings-card 3.1, C-8): read from
// the archive's own record, never from whether Microsoft 365 is set up on this computer.
const (
	backupM365      = "m365"      // the record says Microsoft 365, mixed or empty
	backupLocal     = "local"     // the record says local mail files only
	backupEncrypted = "encrypted" // sealed: never scheduled (phase3 O5)
	backupUnknown   = "unknown"   // the record can't be read: nothing is decided on a guess
)

// backupKind is the kind the backup options and the install route follow for the archive at
// out (3.1, MA-856). A record that can't be read — the folder gone (its drive unplugged), the
// record there but unreadable — is Unknown, never Microsoft 365 (C-8). A folder that is there
// and holds no record at all is a new archive with no source recorded yet, which is empty, so
// Microsoft 365 (an empty archive keeps Archive now and the capture: the owner may be about to
// add a mailbox). The record is untrusted (C-17): it only picks the form and the route; every
// write still passes reconstructLocalJob's checks and the kind guard inside schedule.Install.
func backupKind(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return backupUnknown
	}
	switch archiveKind(out) {
	case kindEncrypted:
		return backupEncrypted
	case kindLocal:
		return backupLocal
	case kindM365, kindMixed, kindEmpty:
		return backupM365
	}
	if fi, err := os.Stat(out); err == nil && fi.IsDir() {
		if _, err := os.Lstat(filepath.Join(out, manifestName)); os.IsNotExist(err) {
			return backupM365
		}
	}
	return backupUnknown
}

// What a broken run-job's job file is (section 5, C-9): the descriptor records a run-job, and
// no run-job is corroborated because the default-named job file is missing or can't be read.
const (
	brokenMissing    = "missing"
	brokenUnreadable = "unreadable"
)

// backupRoute is section 3.2's table for one archive: its record kind against what is
// installed, from owner-side evidence first. A descriptor's kind is read only to add a refusal
// (a command-line export, a run-job where a capture is counted) or to name a broken run-job,
// whose Repair runs the create path, which passes every check a create does.
type backupRoute struct {
	kind string
	// captureCounted is today's owner-side evidence counting a capture for this archive
	// (BuildKindEvidence through CaptureCorroborates): Microsoft 365 is set up and the archive
	// is the current folder or BackupFor. The kind guard reads the same evidence.
	captureCounted bool
	runJob         bool   // a run-job is corroborated: its job file reads securely, naming this archive
	jobPath        string // that job file
	hasDesc        bool
	desc           schedule.Descriptor
	descKind       schedule.Kind
	// notice is section 4's case: a capture is counted and the archive or its schedule is
	// local. Part A shows the notice and refuses a local install there, before Install.
	notice bool
	// export is a command-line export installed here: Settings shows it read-only (C-7).
	export bool
	// broken, brokenPath and brokenReason name a broken run-job (C-9).
	broken, brokenPath, brokenReason string
}

// routeFor reads section 3.2's table for the archive at out. It reads the archive's record and
// descriptor and the owner's own files only — never a mail source's storage.
func routeFor(out string) backupRoute {
	r := backupRoute{kind: backupKind(out)}
	if strings.TrimSpace(out) == "" {
		return r
	}
	if d, err := schedule.ReadDescriptor(out); err == nil {
		r.hasDesc, r.desc, r.descKind = true, d, schedule.KindOf(d.Job)
	}
	if r.kind == backupUnknown {
		return r
	}
	ev := schedule.BuildKindEvidence(out)
	r.captureCounted = ev.Has(schedule.KindCapture)
	r.runJob, r.jobPath = ev.Has(schedule.KindRunJob), ev.JobPath
	if r.kind == backupEncrypted {
		return r // as today: status and Remove only
	}
	descRunJob := r.hasDesc && r.descKind == schedule.KindRunJob
	if !r.runJob && descRunJob {
		if p, err := job.PathFor(schedule.DefaultNameFor(out)); err == nil {
			_, exists, reason := readJobToEdit(p, out)
			switch {
			case reason != "":
				r.broken, r.brokenPath, r.brokenReason = brokenUnreadable, p, reason
			case !exists:
				r.broken, r.brokenPath = brokenMissing, p
			}
		}
	}
	r.notice = r.captureCounted && (r.kind == backupLocal || r.runJob || descRunJob)
	r.export = !r.runJob && (ev.Has(schedule.KindExport) || (r.hasDesc && len(r.desc.Job) > 0 && r.descKind == schedule.KindExport))
	return r
}

// Section 6's refusals and section 4's notice, in plain words (X2).
const (
	unknownKindRefusal  = "MailArchive can't read this archive's record right now (is its drive connected?). Nothing was changed."
	unknownKindLine     = "MailArchive can't read this archive's record right now (is its drive connected?); its backup options show when it can."
	captureCountedLine  = "This archive can't get its own scheduled backup yet on a computer where Microsoft 365 is set up: MailArchive can't yet tell it apart from a Microsoft 365 capture. A later MailArchive update adds it; nothing about this archive is changed meanwhile."
	captureCountedRefus = "This archive can't get its own scheduled backup yet on a computer where Microsoft 365 is set up. Nothing was changed."
	keepsRunningLine    = "Its installed backup keeps running as it is."
	pausedRefusal       = "This archive is paused; Resume it first. Nothing was changed."
	staleRefusal        = "This page is out of date; reload it. Nothing was changed."
	runningRefusal      = "A backup is running now; save again when it finishes. Nothing was changed."
	commandLineRefusal  = "This schedule was set up at the command line; change it there. Nothing was changed."
	// uncountedCaptureRefusal: a local archive whose recorded schedule is a Microsoft 365 capture
	// that is not counted here (MA-878): it is never replaced by a local backup from Settings.
	uncountedCaptureRefusal = "This archive's installed backup is a Microsoft 365 capture, and Microsoft 365 isn't set up on this computer: remove it on the Scheduled backup card before setting up a backup of its mail files. Nothing was changed."
	recreatedNote           = "This archive's backup settings file was missing, so it was made again from this archive's own record."
	syncNowOffNote          = " Thunderbird sync is now off; switch it on in Settings."
)

// brokenLine is the card's words for a broken run-job (section 5, C-9): a missing job file with
// the Repair that makes it again, or an unreadable one with section 6's message and its remedy
// (no Repair: C-6 would refuse it), ahead of any other label.
// A backup run only on Run now (MA-949) is named as the person's choice, never as a scheduled
// backup.
func brokenLine(r backupRoute) string {
	what := "This archive's scheduled backup"
	if r.manualBroken() {
		what = "This archive's backup (" + manualOptionLabel + ")"
	}
	switch r.broken {
	case brokenMissing:
		return what + " can't run: its settings file " + r.brokenPath + " is missing. Repair makes it again from this archive's own record."
	case brokenUnreadable:
		return what + " can't run. This archive's backup settings file " + r.brokenPath + " can't be read (" + r.brokenReason + "). Re-create the backup in the MailArchive wizard, or update MailArchive if a newer version made it."
	}
	return ""
}

// manualBroken reports a broken run-job whose record says "Only when I click Run now" (MA-949).
// It changes only the card's words: what the Repair does follows the record either way.
func (r backupRoute) manualBroken() bool {
	return r.broken != "" && r.hasDesc && r.desc.IsManual()
}

// cadenceWords says a schedule's cadence in plain words: "daily at 04:00", "weekly on Sunday
// at 03:00", or for hourly the minutes past each hour it runs at (an hourly entry uses only
// the minutes of its time).
func cadenceWords(interval, at string) string {
	at = strings.TrimSpace(at)
	switch strings.ToLower(strings.TrimSpace(interval)) {
	case string(schedule.Manual):
		return "only when you click Run now" // MA-949: no time is used
	case "hourly":
		mm := at
		if i := strings.LastIndex(at, ":"); i >= 0 {
			mm = at[i+1:]
		}
		return "every hour, at " + mm + " minutes past"
	case "daily":
		return "daily at " + at
	case "weekly":
		return "weekly on Sunday at " + at
	}
	return strings.TrimSpace(interval) + " at " + at
}

// installedSyncWords is an installed run-job's Thunderbird sync as Settings shows it read-only:
// on, off, or can't read with the reason — never "off" for a file that can't be read (C-9).
func installedSyncWords(path, out string) string {
	if path == "" {
		return "can't read (this archive's backup settings file can't be found)"
	}
	j, exists, reason := readJobToEdit(path, out)
	switch {
	case reason != "":
		return "can't read (" + reason + ")"
	case !exists:
		return "can't read (its settings file " + path + " is missing)"
	case j.ThunderbirdSync != nil:
		return "on"
	}
	return "off"
}

// isRemotePath reports a path on another computer or one that is not a plain local path: a
// network (UNC) path, a device path, or one that is not absolute. A mail source named so in the
// archive's record — which anyone who can write the archive can change — is skipped before any
// file check, so drawing the page never reaches out to another host (C-17, MA-869).
func isRemotePath(p string) bool {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//") {
		return true
	}
	return !filepath.IsAbs(p)
}

// sourceWait bounds how long drawing the page waits on a mail source's storage (C-17, MA-869):
// the local-backup form checks that the archive's sources are there, and a source on slow or
// stalled storage must not hold the page.
var sourceWait = 2 * time.Second

// withinSourceWait runs f, a check that touches a mail source's storage, and gives its result
// when it finishes within sourceWait; otherwise ok is false and the page is drawn without it (f
// still finishes on its own, and its result is dropped).
func withinSourceWait[T any](f func() T) (v T, ok bool) {
	ch := make(chan T, 1)
	go func() { ch <- f() }()
	t := time.NewTimer(sourceWait)
	defer t.Stop()
	select {
	case v = <-ch:
		return v, true
	case <-t.C:
		return v, false
	}
}
