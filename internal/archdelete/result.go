package archdelete

import (
	"fmt"
	"strings"
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
	// that may not be changed.
	CauseDenied
	// CauseDeviceGone is a drive that went away.
	CauseDeviceGone
)

// remedy is §8 (a)'s remedy for a cause.
func (c Cause) remedy() string {
	switch c {
	case CauseInUse:
		return "Close Outlook or any program using them, then click Delete again."
	case CauseDenied:
		return "Windows security or antivirus blocked it: allow MailArchive or delete these by hand."
	case CauseDeviceGone:
		return "Reconnect the drive, then click Delete again."
	}
	return "Close any program using them, then click Delete again."
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
	// Steps are the steps that changed something, in the words "Steps done" uses.
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

// msgChanged is §3.1's re-check refusal.
const msgChanged = "This archive changed since you clicked Delete. Click Delete again."

// msgLogFailed is step 0's stop.
const msgLogFailed = "Could not write MailArchive's deletion log, so nothing was deleted."

// msgSettingsFailed is step 4's stop.
const msgSettingsFailed = "Could not save MailArchive's settings, so this archive is still chosen. Click Delete again."

// msgKeySkipped is step 8's skipped line.
const msgKeySkipped = "Files deleted; this computer still remembers its key: use Forget this encrypted archive here."

// msgRowFailed is step 9's failure line.
const msgRowFailed = "Could not remove the row from Your archives."

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
