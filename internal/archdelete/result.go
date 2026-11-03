package archdelete

import (
	"fmt"
	"strings"

	"mail-archive-tool/internal/util"
)

// StatusRefused is a delete refused before its started line: nothing was changed
// and nothing was logged (a name that does not match, a log that cannot be
// written).
const StatusRefused = "refused"

// Cause is what kind of error kept a file, for its remedy (§8 (a)).
type Cause int

const (
	// CauseOther is any other error.
	CauseOther Cause = iota
	// CauseInUse is a sharing violation: a program holds the file open.
	CauseInUse
	// CauseDenied is access denied: Windows security, an antivirus, a read-only file
	// that may not be changed; on macOS and Linux a permission refused (deniedRemedy
	// is each platform's words).
	CauseDenied
	// CauseDeviceGone is a drive that went away.
	CauseDeviceGone
)

// remedy is §8 (a)'s remedy for a cause, for n files left: "it" for one, "them" or "these" for
// more (the 1.5.3 friction review's B7). where names the folders of the files refused, for
// CauseDenied on macOS and Linux ("" names none; B6).
func (c Cause) remedy(n int, where string) string {
	them := "them"
	if n == 1 {
		them = "it"
	}
	switch c {
	case CauseInUse:
		return "Close Outlook or any program using " + them + ", then click Delete again."
	case CauseDenied:
		r := deniedRemedy
		if n == 1 {
			r = strings.Replace(r, "delete these by hand", "delete it by hand", 1)
		}
		if where != "" {
			r = strings.Replace(r, "The system did not allow it", "The system did not allow it in "+where, 1)
		}
		return r
	case CauseDeviceGone:
		return "Reconnect the drive, then click Delete again."
	}
	return "Close any program using " + them + ", then click Delete again."
}

// LeftFile is a file Delete was to remove and could not (§8 (a)).
type LeftFile struct {
	// Rel is the file, relative to the archive folder with forward slashes.
	Rel   string
	Cause Cause
	Err   string
}

// Result is what a delete did (§8): its status, the result lines in order, and
// the facts behind them.
type Result struct {
	// Status is StatusComplete, StatusPartial, StatusRefusedAfterStart or
	// StatusRefused.
	Status string
	// Refusal is the refusal when the delete was refused (before its started line,
	// or under the lock).
	Refusal *Refusal
	// Lines are the result's lines, in order; Message joins them.
	Lines []string
	// Removed and Bytes count the archive's files removed; LitterRemoved counts §8 (b).
	Removed       int
	Bytes         int64
	LitterRemoved int
	// NotRemoved are §8 (a)'s names the archive recorded or owns; TickedLeft the §4.3
	// files the person ticked that could not be removed.
	NotRemoved []LeftFile
	TickedLeft []LeftFile
	// Kept are §8 (c)'s: the person's files, and §4.3's when unticked.
	Kept []string
	// FolderGone: the archive folder was removed.
	FolderGone bool
	// LockFileLeft: .mailarchive.lock could not be removed (step 7, step 8).
	LockFileLeft bool
	// Stopped are the scheduler entries this delete removed; CheckYourself those the
	// plan lists as "not removed: check it yourself".
	Stopped       []string
	CheckYourself []string
	// AlreadyAbsent are the entries the plan listed that step 2 found gone; RunNowRemoved the Run
	// now backups whose job files step 3 removed (B1, B15).
	AlreadyAbsent []string
	RunNowRemoved []string
	// KeyForgotten is "forgotten", "kept: …" or "" (not applicable or skipped).
	KeyForgotten string
	// KeySkipped: step 8 was skipped and this computer still remembers the key.
	KeySkipped bool
	// RowRemoved: step 9 removed the row.
	RowRemoved bool
	// Unfinished: the row now shows "Delete not finished: click Delete again" (§5.4).
	Unfinished bool
	// Record is the deletion record's exact bytes (§8.1), built once before step 10;
	// nil when the delete was refused before its started line.
	Record []byte
	// Steps are the steps done, in order, in the words "Steps done" uses ("archive
	// locked", "scheduled backup NAME stopped", "N files removed", …); empty when the
	// delete was refused before its started line.
	Steps []string
}

// ExitCode is the command line's exit code (§3.3): 0 deleted, 3 not fully deleted,
// 1 refused.
func (r *Result) ExitCode() int {
	switch r.Status {
	case StatusComplete:
		return 0
	case StatusPartial:
		return 3
	}
	return 1
}

// Message is the result's lines joined, one per line.
func (r *Result) Message() string { return strings.Join(r.Lines, "\n") }

func (r *Result) line(format string, args ...any) {
	r.Lines = append(r.Lines, fmt.Sprintf(format, args...))
}

// lockedLine is added to every stop from step 1 (b) on (§5.3 step 1 [R3:C13]).
const lockedLine = "This archive was locked on this page; unlock it again to read it."

// The plan's fixed sentences, shared by the Desktop's panel and the command line's plan
// (§3.1, §6, §9; slice 4).
const (
	// PlanLogLine is §3.1's deletions.log line, said before anything is deleted, so "if" (the
	// 1.5.3 friction review's B5: a plan-only run said a line "is added").
	PlanLogLine = "If you delete it, a line is added to this computer's deletion log (deletions.log), which keeps the date, the folder and what was removed."
)

// PlanLogLineAt is PlanLogLine naming the log's full path (the review's item 9), "" path being
// PlanLogLine.
func PlanLogLineAt(path string) string {
	if path == "" {
		return PlanLogLine
	}
	return "If you delete it, a line is added to this computer's deletion log (" + util.StripControl(path) + "), which keeps the date, the folder and what was removed."
}

// KeyLine is §6's line for an encrypted archive [C8], picked at the plan from the keyring's
// forget plan (§14, slice 5's re-verification, residual 2): step 8 forgets the key only when the
// folder is removed and this is the last copy this computer recorded; otherwise the key is kept
// for the other recorded copies, which it names. With no secret store the last copy's record,
// any remembered key and the row are kept (MA-999). "" for a plain archive.
func (p *DeletePlan) KeyLine() string {
	switch {
	case p.Kind != Encrypted:
		return ""
	case p.KeyForgottenBefore:
		return "This computer already forgot this archive folder's record in the delete this one finishes."
	case !p.Record.Recorded && !p.RecordUnknown:
		return "This computer has not recorded this archive folder, so Delete forgets no key here."
	case p.KeyAtPlan.Known && p.KeyAtPlan.Last && p.KeyAtPlan.NoStore:
		return "This computer's secret store could not be opened, so this computer will keep its record of this archive, any key it remembers for it and its row, even if the folder is removed. Use Forget this encrypted archive here later."
	case p.KeyAtPlan.Known && p.KeyAtPlan.Last && !p.KeyAtPlan.Remembered:
		// No key is remembered here: only the record goes (B3).
		return "If the folder is removed, this computer forgets its record of this archive (no key is remembered here)."
	case p.KeyAtPlan.Known && p.KeyAtPlan.Last:
		return "If the folder is removed, this computer will forget this archive's key: this is the last copy of it this computer recorded. Any other copy of this archive will then need its password or recovery key to open."
	case p.KeyAtPlan.Known:
		others := make([]string, 0, len(p.KeyAtPlan.Others))
		for _, o := range p.KeyAtPlan.Others {
			others = append(others, util.StripControl(o))
		}
		return "This computer keeps this archive's key for its other recorded copies: " + strings.Join(others, "; ") + ". If the folder is removed, it forgets only this copy."
	}
	return "If the folder is removed and it is the last copy of this archive this computer recorded, this computer will forget the archive's key; otherwise the key is kept for the other copies."
}

// FinishesLine is the plan's first line when it finishes a delete of this folder that did not
// finish (the 1.5.3 friction review's D1), on the Desktop's panel and at the terminal: "This
// finishes the delete begun DATE." ("" for an ordinary delete).
func (p *DeletePlan) FinishesLine() string {
	if p.Finishes == nil {
		return ""
	}
	if p.Finishes.FirstStarted.IsZero() {
		return "This finishes a delete of this folder that did not finish."
	}
	return "This finishes the delete begun " + localStamp(p.Finishes.FirstStarted) + "."
}

// The plan's sentences after its first line when it finishes a delete this computer's log did not
// see begin, or one finished elsewhere (§15.4, DM-28, §15.8 question 2; lead's words,
// owner-delegated 2026-11-02: what happened first, no "again" to a person who began no delete).
const (
	// MsgMarkerNoRecord follows "This finishes the delete begun DATE." for a delete planned from the
	// folder's marker: this computer's log has no line of its start.
	MsgMarkerNoRecord = "This computer's deletion log has no record of when it began: it was begun on another computer or user account."
	// MsgMarkerBackupAfter is DM-28's: the archive's last backup is later than the marker's began
	// (words only: the two times may come from two clocks).
	MsgMarkerBackupAfter = "A backup ran into this archive after its delete began, probably from a computer with an older MailArchive."
	// MsgElsewhereSaid is the plan's and the result's line for a delete the person said was finished
	// elsewhere (MA-1057).
	MsgElsewhereSaid = "The folder was not found here; you said this delete was finished elsewhere."
)

// elsewhereValue is the deletion record's "Folder here" value for a delete finished elsewhere.
const elsewhereValue = "the folder was not found here; you said this delete was finished elsewhere"

// FinishesNotes are the plan's sentences after FinishesLine, on the Desktop's panel and at the
// terminal: none for an ordinary delete or one this computer's log saw begin.
func (p *DeletePlan) FinishesNotes() []string {
	var out []string
	if p.Elsewhere {
		out = append(out, MsgElsewhereSaid)
	}
	if f := p.Finishes; f != nil && f.FromMarker {
		out = append(out, MsgMarkerNoRecord)
		// DM-28: a backup into the archive after its delete began (an older MailArchive elsewhere).
		if began := f.FirstStarted; !began.IsZero() && p.Captured.LastBackup.After(began) {
			out = append(out, MsgMarkerBackupAfter)
		}
	}
	return out
}

// finishesValue is the saved plan's and the record's "Finishes" line: the first run's date and id.
// One planned from the folder's marker says the marker is its source, which MailArchive cannot
// confirm (DM-8).
func (p *DeletePlan) finishesValue() string {
	f := p.Finishes
	if f.FromMarker {
		const asNoted = ", as the folder's note says; MailArchive cannot confirm it"
		if f.FirstRun == "" {
			return "a delete of this folder that did not finish" + asNoted
		}
		if f.FirstStarted.IsZero() {
			return fmt.Sprintf("the delete begun on a date not recorded (run %s)%s", f.FirstRun, asNoted)
		}
		return fmt.Sprintf("the delete begun %s (run %s)%s", localStamp(f.FirstStarted), f.FirstRun, asNoted)
	}
	when := "on a date not recorded"
	if !f.FirstStarted.IsZero() {
		when = localStamp(f.FirstStarted)
	}
	return fmt.Sprintf("the delete begun %s (run %s)", when, f.FirstRun)
}

// earlierValue is the record's "Files removed earlier": what the runs before this one removed. A
// run that stopped with no finished line (Ctrl-C at the terminal, a crash) wrote no count, so when
// no earlier run's count is known the value is "not known", never "at least 0" (the 1.5.3
// re-walk's N2, the operator's delegate's choice of its two forms).
func (p *DeletePlan) earlierValue() string {
	f := p.Finishes
	why := p.notKnownWhy()
	switch {
	case !f.RemovedKnown && f.Removed == 0:
		return "not known: " + why
	case !f.RemovedKnown:
		return fmt.Sprintf("at least %d (%s); not known in full: %s", f.Removed, size(f.RemovedBytes), why)
	}
	return fmt.Sprintf("%d (%s)", f.Removed, size(f.RemovedBytes))
}

// notKnownWhy is why an earlier run's count is not known.
const notKnownWhy = "a run stopped before it wrote its record"

// notKnownMarkerWhy is why it is not known for a delete planned from the folder's marker (DM-8).
const notKnownMarkerWhy = "the delete was begun where this computer's deletion log does not see it"

// notKnownWhy is why the earlier runs' count of the delete p finishes is not known.
func (p *DeletePlan) notKnownWhy() string {
	if p.Finishes != nil && p.Finishes.FromMarker {
		return notKnownMarkerWhy
	}
	return notKnownWhy
}

// finishedLine is the result's line of a delete that finishes one that stopped part way: the
// whole delete's count, beside this run's own in the line before it (the 1.5.3 re-walk's N6: the
// total was only in the record). removed and bytes are this run's.
func (p *DeletePlan) finishedLine(removed int, bytes int64) string {
	f := p.Finishes
	begun := "This finishes a delete of this folder that did not finish"
	if !f.FirstStarted.IsZero() {
		begun = "This finishes the delete begun " + localStamp(f.FirstStarted)
	}
	if !f.RemovedKnown {
		return begun + ". How many files its earlier runs removed is not known: " + p.notKnownWhy() + "."
	}
	return fmt.Sprintf("%s: %s (%s) removed in all.", begun, plural(f.Removed+removed, "file", "files"), size(f.RemovedBytes+bytes))
}

// HeldLine is the plan's warning, on the Desktop's panel and at the terminal, that files of a delete
// it finishes are still held open by another program, so this run would stop at them again (the
// 1.5.3 re-walk's N17: a re-run made while the file was still held said nothing until it failed);
// "" when none is.
func (p *DeletePlan) HeldLine() string {
	switch n := len(p.Held); {
	case n == 0:
		return ""
	case n == 1:
		return "1 file this delete must remove is still in use by another program: " + namesAndMore(p.Held) +
			". Close Outlook or any program using it before you delete, or this delete will stop at it again."
	default:
		return fmt.Sprintf("%d files this delete must remove are still in use by another program: %s. Close Outlook or any program using them before you delete, or this delete will stop at them again.", n, namesAndMore(p.Held))
	}
}

// KeptLine is the plan's line naming the files and folders Delete keeps as not the archive's, on
// the Desktop's panel and at the terminal: the first ten, then "and N more" (B6, MA-1019; the
// 1.5.3 re-walk's N7: the terminal said only how many). "" when it keeps none.
func (p *DeletePlan) KeptLine() string {
	if len(p.Kept) == 0 {
		return ""
	}
	return "Kept (not the archive's): " + namesAndMore(p.Kept)
}

// namesAndMore is the first ten names, control characters stripped, then " and N more", as the
// panel words its kept files.
func namesAndMore(names []string) string {
	shown := names
	if len(shown) > recordListMax {
		shown = shown[:recordListMax]
	}
	out := make([]string, 0, len(shown))
	for _, n := range shown {
		out = append(out, util.StripControl(n))
	}
	s := strings.Join(out, ", ")
	if more := len(names) - len(shown); more > 0 {
		s += fmt.Sprintf(" and %d more", more)
	}
	return s
}

// StoreLabel is mail folder i's (0-based) label on screen: its folder in the archive, and the mail
// folders the archive recorded inside it when they are named otherwise ("mbox (Inbox, Sent)"), the
// first ten then "and N more" (the 1.5.3 re-walk's N16: "mail folder 1: mbox" named the source's
// folder, not the Inbox the person knows). On screen only: the saved plan and the record name no
// folder below a store [O2-01].
func (p *DeletePlan) StoreLabel(i int) string {
	tok := util.StripControl(p.Stores[i])
	var inner []string
	if i < len(p.StoreFolders) {
		for _, f := range p.StoreFolders[i] {
			if f != p.Stores[i] {
				inner = append(inner, f)
			}
		}
	}
	if len(inner) == 0 {
		return tok
	}
	return tok + " (" + namesAndMore(inner) + ")"
}

// ScheduleLines are the plan's lines about the archive's backups, shared by the Desktop's panel
// and the command line (the 1.5.3 friction review's B1, B15): the scheduled backups step 2 would
// stop, by when they run ("Scheduled backups it stops: the backup that runs daily at 02:00."), or
// "none" — never for an encrypted archive, which cannot have one — and a Run now backup, which is
// this archive's own and has no scheduled times, removed with its job file.
func (p *DeletePlan) ScheduleLines() []string {
	var descs []string
	for _, e := range p.Schedules.Listed() {
		descs = append(descs, e.Describe())
	}
	var lines []string
	switch {
	case len(descs) > 0:
		lines = append(lines, "Scheduled backups it stops: "+strings.Join(descs, "; ")+".")
	case p.Kind != Encrypted:
		lines = append(lines, "Scheduled backups it stops: none.")
	}
	if len(p.Schedules.RunNowBackups()) > 0 {
		lines = append(lines, "Its Run now backup is removed too (it has no scheduled times).")
	}
	return lines
}

// ScheduleDescriptions are the scheduled backups step 2 would stop, each as Describe says it.
func (p *DeletePlan) ScheduleDescriptions() []string {
	out := []string{}
	for _, e := range p.Schedules.Listed() {
		out = append(out, e.Describe())
	}
	return out
}

// SyncedLine is §9's warning for a folder synced with svc [C14].
func SyncedLine(svc string) string {
	return fmt.Sprintf("This folder is synced with %s. Deleting it here also deletes it there, and %s may keep it in its own recycle bin: empty that too.", svc, svc)
}

// msgChanged is §3.1's re-check refusal.
const msgChanged = "This archive changed since you clicked Delete. Click Delete again."

// msgLogFailed is step 0's stop.
const msgLogFailed = "Could not write MailArchive's deletion log, so nothing was deleted."

// msgSettingsFailed is step 4's stop.
const msgSettingsFailed = "Could not save MailArchive's settings, so this archive is still chosen. Click Delete again."

// msgKeySkipped is step 8's skipped line.
const msgKeySkipped = "Files deleted; this computer still remembers its key: use Forget this encrypted archive here."

// msgKeySkippedNoKey is step 8's skipped line when no key is remembered here (B3; builder's words).
const msgKeySkippedNoKey = "Files deleted; this computer still keeps its record of this archive (no key is remembered here): use Forget this encrypted archive here."

// msgRowFailed is step 9's failure line.
const msgRowFailed = "Could not remove the row from Your archives."

// msgRowNotListed is step 9's line when Your archives lists no row for the folder
// (del-s1b-A3-06; builder's words, §5.3 step 9 gives none).
const msgRowNotListed = "No row for this folder was found in Your archives, so none was removed."

// msgDigestFailed is step 10's failure line.
const msgDigestFailed = "This record's digest could not be added to deletions.log."

// msgLockFileLeft is step 7's lock-file line [R3:NF-C1-RESULT].
const msgLockFileLeft = "One MailArchive lock file (.mailarchive.lock) could not be removed: delete it by hand."

// msgEmptyFolderLeft is §5.3 "After step 7"'s line for a kept folder that is empty
// or holds only litter.
const msgEmptyFolderLeft = "The empty folder could not be removed: delete it by hand."

// The §7 rows step 1 (c) and step 5 can refuse with [C7].
const (
	msgReaderOpen = "Close MailArchive serve, search or other MailArchive windows for this archive first."
)
