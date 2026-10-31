package desktop

import (
	"strings"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// settingsCard is what the Settings card shows for this archive (design-desktop-settings-card
// 3.3, slice A5). Settings is the one place to change how this archive is backed up (owner
// ruling 1): how often, the time and the Thunderbird sync, showing only what applies to its
// kind. The Scheduled backup card keeps the status with Run now, Repair and Remove.
type settingsCard struct {
	// M365Boxes: the three Microsoft 365 boxes, only on a Microsoft 365 archive with the
	// Microsoft 365 fields (ruling 3), labelled as this computer's (C-16).
	M365Boxes bool
	// ArchiveNowUses: the line for an archive where Archive now is offered and the boxes are
	// hidden (R3C-16), so the options Archive now uses are never out of sight.
	ArchiveNowUses bool
	// Eml is the Local fields' read-only "keep .eml" value (ruling 3); "" when not shown.
	Eml string
	// Unknown is why only the location shows: the archive's record can't be read (C-8).
	Unknown string
	// Sched: How often and At are drawn. SchedNone offers "No scheduled backup" (nothing
	// installed); SchedHourly offers Hourly (the form edits a run-job, OR-2); SchedCapture is a
	// Microsoft 365 capture's form.
	Sched, SchedNone, SchedHourly, SchedCapture bool
	// SchedManual offers "Only when I click Run now" (MA-949): wherever the form edits a
	// run-job, as Hourly is; never on a capture's form. ManualHint is shown under it.
	SchedManual bool
	ManualLabel string
	ManualHint  string
	// Interval and At are the installed schedule's; with nothing installed, none and 03:00 —
	// never the settings file's copy (3.3). Installed says a schedule is recorded.
	Interval, At string
	Installed    bool
	// CadenceUnknown: a capture is installed but its recorded cadence can't be read (its record
	// on a drive that is unplugged, or gone): How often shows "As installed" selected, which a
	// Save never posts, so an unrelated Save never changes the cadence (MA-876).
	CadenceUnknown bool
	// MustPost: the installed entry is gone (auto-pause took it out, or the scheduler lost
	// it), so Save posts the schedule even with the cadence unchanged (C-11, 3.6 step 3).
	MustPost bool
	// AutoPaused is the "Backups stopped" line of an auto-paused archive (3.4).
	AutoPaused string
	// The Thunderbird sync box (C-10): drawn for an archive made from Thunderbird and
	// whenever the installed job carries the sync; SyncReason says why it can't run now.
	Sync       bool
	SyncOn     bool
	SyncReason string
	// SyncNotice and MacNote are shown, visible, beside the sync box (X16, MA-861).
	SyncNotice string
	MacNote    string
	// SyncOnly: an encrypted archive's sync-only box, with its own Save posting to
	// api/runnow-sync (design-presync-attended §8.1, C-26); SyncLast is the last Run
	// now's sync while the archive is unlocked here (C-27); SyncUnreadable says why the
	// box can't be shown when the Run now job can't be read.
	SyncOnly       bool
	SyncLast       string
	SyncUnreadable string
	// ReadOnly is an installed schedule shown read-only, and Remedy how to change it.
	ReadOnly, Remedy string
}

// The Settings card's own words (X2, X11).
const (
	m365BoxesLine      = "These apply to every Microsoft 365 capture on this computer, and to Archive now."
	archiveNowUsesLine = "Archive now uses this computer's Microsoft 365 options (Deleted Items, Junk Email, original .eml); change them in Settings on a Microsoft 365 archive."
	emlYes             = "Yes (set by how this archive was made)"
	emlNo              = "No (set by how this archive was made)"
	emlNotKnown        = "Not known"
	syncCantRunLine    = "Thunderbird's profile isn't on this computer right now, so the sync can't run until it is back."
	syncFirstLine      = "Choose how often first: the sync runs before each backup."
	moveFirstLine      = "Save the new location first; this archive's options then show here."
	autoPausedSaveLine = "Saving starts the scheduled backup again. If the source is still unreachable at the next backup, it stops again."
	// wizardRemedy is the remedy for a schedule Settings shows read-only, naming what the wizard
	// really offers (R3C-16, wizard.go offerSchedule).
	wizardRemedy     = "To change it, run the MailArchive wizard (mailarchive wizard), which offers daily at 2:00 AM or weekly on Sunday at 3:00 AM, or use the command line's mailarchive schedule with -at."
	commandLineSetUp = "This schedule was set up at the command line; change it there."
	// commandLineToRunJob follows commandLineSetUp where removing the schedule lets this
	// card make one the Desktop can run (MA-942): Save settings alone is refused (C-7).
	commandLineToRunJob = " To get one MailArchive Desktop can run, with Run now, click Remove on the Scheduled backup card, then choose How often here and click Save settings."
	// The Scheduled backup card's line for a schedule set up at the command line (MA-942):
	// why Run now isn't shown, and how to get it where this page can.
	exportGetRunNowLine = "Run now isn't shown for it: MailArchive Desktop can't run or change a backup set up at the command line. To get Run now, click Remove below (your archive and its mail are kept), then in Settings choose How often and click Save settings. That sets up a backup MailArchive Desktop can run, and Run now then appears here."
	exportNoRunNowLine  = "It was set up at the command line, so change or repair it there; MailArchive Desktop can only remove it (Remove below)."
	brokenMissingFix    = "Click Repair on the Scheduled backup card to make its settings file again; its options then show here."
	brokenUnreadableAt  = "See the Scheduled backup card for what to do."
	archiveNowConfirm   = "Archive now adds Microsoft 365 mail to this archive. It then counts as mixed and shows the Microsoft 365 options; a scheduled backup of its mail files keeps running."
)

// exportCarryOverLine follows exportGetRunNowLine (friction review L4): how the schedule set
// up at the command line runs now, and that Remove does not keep it, so the person can choose
// the same again in Settings (after Remove, Settings starts from no schedule, sync off).
func exportCarryOverLine(d schedule.Descriptor) string {
	if d.Presync != nil {
		return "It runs " + cadenceWords(d.Interval, d.At) + " now, with the Thunderbird sync on. Remove does not keep that: after Remove, choose How often and the time again in Settings, and tick Sync Thunderbird before each backup."
	}
	return "It runs " + cadenceWords(d.Interval, d.At) + " now. Remove does not keep that: after Remove, choose How often and the time again in Settings."
}

// settingsInputs is what the page already worked out for the Scheduled backup card, which the
// Settings card follows (the same route, 3.2).
type settingsInputs struct {
	out     string
	rt      backupRoute
	entry   string         // the archive folder the scheduler's entry is named for ("" none)
	entrySt schedule.State // what the scheduler said for this archive's own name
	// runJobForm and captureForm: which form section 3.2's table gives this archive.
	runJobForm, captureForm bool
	// canEdit: the run-job form can be drawn here (macOS or Windows; for an edit, a securely
	// readable job file; for a create, the archive's own record rebuilds: 3.3, R3C-16).
	canEdit bool
	// thunderbird: the archive was made from a Thunderbird profile that is on this computer.
	thunderbird       bool
	goos              string
	archiveNowOffered bool
	// exportToRunJob: a command-line schedule of a local archive that, once removed, this
	// card can replace with one the Desktop runs (MA-942).
	exportToRunJob bool
	// exportOwnName: that schedule was set up under a name of its own, which the card's
	// Remove does not reach (MA-948).
	exportOwnName bool
}

// settingsCardFor works out the Settings card for one archive (3.2, 3.3).
func settingsCardFor(in settingsInputs) settingsCard {
	var s settingsCard
	rt, out := in.rt, in.out
	switch {
	case out == "":
		// No archive yet: the boxes as before, and the capture's form, whose Save asks for a
		// location first.
		s.M365Boxes = true
	case rt.kind == backupEncrypted:
		return s // the location only (3.2)
	case rt.kind == backupUnknown:
		s.Unknown = unknownKindLine // the location only, and why (C-8)
		return s
	default:
		localFields := rt.kind == backupLocal || (!rt.captureCounted && (rt.runJob || rt.broken != ""))
		s.M365Boxes = !localFields
		if localFields {
			s.Eml = emlWords(out, rt)
		}
		s.ArchiveNowUses = localFields && in.archiveNowOffered
	}
	jp := rt.jobPath
	if jp == "" && out != "" {
		jp, _ = job.PathFor(schedule.DefaultNameFor(out))
	}
	howOften := ""
	if rt.hasDesc {
		howOften = "How often: " + cadenceWords(rt.desc.Interval, rt.desc.At) + "."
	}
	switch {
	case in.runJobForm && in.canEdit:
		s.Sched, s.SchedHourly, s.SchedNone = true, true, !rt.hasDesc
		s.SchedManual, s.ManualLabel, s.ManualHint = true, manualOptionLabel, manualHint
		installedSync := rt.runJob && currentJobSyncsThunderbird(out)
		s.Sync, s.SyncOn = in.thunderbird || installedSync, installedSync
		if installedSync && !in.thunderbird {
			s.SyncReason = syncCantRunLine // never hidden while installed (C-10)
		}
	case in.captureForm:
		s.Sched, s.SchedCapture = true, true
		s.SchedNone = !rt.hasDesc && in.entry == ""
	case rt.hasDesc && rt.export:
		s.ReadOnly, s.Remedy = howOften, commandLineSetUp // C-7
		switch {
		case in.exportToRunJob && in.exportOwnName:
			s.Remedy += commandLineOwnNameToRunJob
		case in.exportToRunJob:
			s.Remedy += commandLineToRunJob
		}
	case rt.hasDesc && rt.broken == brokenMissing:
		s.ReadOnly, s.Remedy = howOften+" Sync Thunderbird first: "+installedSyncWords(rt.brokenPath, out)+".", brokenMissingFix
	case rt.hasDesc && rt.broken == brokenUnreadable:
		s.ReadOnly, s.Remedy = howOften+" Sync Thunderbird first: "+installedSyncWords(rt.brokenPath, out)+".", brokenUnreadableAt
	case rt.hasDesc && in.runJobForm:
		// An installed run-job Settings can't edit here (Linux, or no securely readable job
		// file): its cadence and sync read-only, and the wizard's real choices.
		s.ReadOnly, s.Remedy = howOften+" Sync Thunderbird first: "+installedSyncWords(jp, out)+".", wizardRemedy
	}
	if s.Sched {
		s.At = "03:00"
		if rt.hasDesc {
			s.Installed, s.Interval, s.At = true, strings.ToLower(strings.TrimSpace(rt.desc.Interval)), orElse(rt.desc.At, "03:00")
		} else if in.captureForm && in.entry != "" {
			// A capture named for the archive's earlier location (Save and keep the backup).
			if d, err := schedule.ReadDescriptor(in.entry); err == nil {
				s.Installed, s.Interval, s.At = true, strings.ToLower(strings.TrimSpace(d.Interval)), orElse(d.At, "03:00")
			}
		}
		s.CadenceUnknown = in.captureForm && in.entry != "" && !s.Installed
		// A backup run only on Run now has no entry to be gone: nothing to put back (MA-949).
		if s.Installed && in.entry == "" && in.entrySt == schedule.NotInstalled && !(rt.hasDesc && rt.desc.IsManual()) {
			s.MustPost = true // the entry is gone: Save puts it back (3.4)
			if st, err := state.ReadSourceStreak(out); err == nil && st.AutoPauseDue() {
				s.AutoPaused = "Backups stopped: this archive's source has been unreachable since " + sinceWords(st) + "."
			}
		}
	}
	if s.Sync {
		// X16: visible text, never hover-only; this platform's, so it says a backup
		// started with Run now does not wait only where Run now starts Thunderbird
		// (design-presync-attended §7, C-84).
		s.SyncNotice = presync.SyncNoticeFor(in.goos)
		if in.goos == "darwin" {
			s.MacNote = presync.MacAutomationNote
		}
	}
	return s
}

// sinceWords is the date a source streak began, as the Your-archives banner says it.
func sinceWords(st state.SourceStreak) string {
	if st.FirstUnreachable.IsZero() {
		return "recently"
	}
	return st.FirstUnreachable.Format("Jan 2, 2006")
}

// emlWords is a local archive's "keep .eml" value, read-only (ruling 3): its installed job
// file's, else the archive's last run's (the owner-checked record), else not known. The value
// shown is the value kept: an edit no longer rebuilds the job (C-4).
func emlWords(out string, rt backupRoute) string {
	if rt.runJob && rt.jobPath != "" {
		if j, err := job.Read(rt.jobPath); err == nil {
			return yesNoEml(j.KeepRaw)
		}
	}
	if args, ok := lastRunJobArgs(out); ok {
		if j, ok := job.FromCanonicalArgs(args); ok {
			return yesNoEml(j.KeepRaw)
		}
	}
	return emlNotKnown
}

func yesNoEml(raw bool) string {
	if raw {
		return emlYes
	}
	return emlNo
}

// scheduleReason is a section 6 refusal as the Settings card quotes it inside its combined
// reply ("<what was saved>. The backup schedule was not changed: <reason>."), without the
// refusal's own "Nothing was changed." (3.6 step 6, R3C-16): the reply never says "saved" and
// "Nothing was changed" together.
func scheduleReason(msg string) string {
	msg = strings.TrimSpace(msg)
	for _, tail := range []string{" Nothing was changed.", ", so nothing was changed."} {
		if strings.HasSuffix(msg, tail) {
			msg = strings.TrimSuffix(msg, tail)
			if !strings.HasSuffix(msg, ".") {
				msg += "."
			}
			return msg
		}
	}
	return msg
}
