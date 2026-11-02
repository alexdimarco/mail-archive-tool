package app

import (
	"errors"
	"sync"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// unfinishedDeleteCheck is the check that a delete of a folder did not finish, by a deletion log
// (MA-1044; the 1.5.3 re-walk's guards pass, G2). It is archdelete's (archdelete.BackupRefusal),
// installed by that package when it is loaded: this package cannot import it, since archdelete
// reaches package app through keyring. Every program that runs a backup links archdelete (the
// command line, the wizard, the Desktop), so the check is there for every run; a test of this
// package alone runs without it, or installs its own.
var (
	unfinishedDeleteMu    sync.RWMutex
	unfinishedDeleteCheck func(logPath, out string) error
)

// SetUnfinishedDeleteCheck installs check as the engine's unfinished-delete check: check(logPath,
// out) returns why a backup must not run into out because a delete of it did not finish by the
// deletion log at logPath ("" is MailArchive's own), or because that log cannot be read; nil when
// nothing stops it. archdelete installs it; a test installs its own and restores the one it
// replaced with the function returned.
func SetUnfinishedDeleteCheck(check func(logPath, out string) error) (restore func()) {
	unfinishedDeleteMu.Lock()
	old := unfinishedDeleteCheck
	unfinishedDeleteCheck = check
	unfinishedDeleteMu.Unlock()
	return func() {
		unfinishedDeleteMu.Lock()
		unfinishedDeleteCheck = old
		unfinishedDeleteMu.Unlock()
	}
}

// RefuseUnfinishedDelete is the refusal of a backup into out because a delete of it did not finish
// (MA-1044, G2): the delete removes the manifest last, so the emptied-folder guard lets a run into
// a folder a delete stopped part way, and the run would write into it. logPath is the deletion log
// to ask ("" is MailArchive's own, as the terminal's pause, resume and schedule ask). The words are
// MA-1015's ("Delete not finished: click Delete again. This archive's delete did not finish, so it
// was not backed up; nothing was changed."), or for a log that cannot be read, its "could not read
// its deletion log" refusal, each followed by " (Folder: DIR)"; the error unwraps to the check's
// own (archdelete.AsRefusal finds it, so the terminal adds the folder and its Delete again itself).
// It is kind delete_unfinished, or deletion_log_unreadable when the check's error says the log
// could not be read (a LogUnreadable() true, as archdelete's refusal of that case says), so an
// encrypted archive's alert says its own fixed sentence, never "connect it" (the G2 settle pass's
// MED-3, MA-1045); a delete still running (its error matches lockfile.ErrHeld) reads as
// archive_busy, as FailureKind names a held lock first. The folder holds no whole archive, so a
// scheduled run's callers take back what they wrote there before the engine's lock and tell the
// refusal outside it, as for the emptied-folder guard (RefusedAsNoArchive; design-archive-delete
// §7). nil when nothing stops the run, or when no check is installed (a test of this package
// alone; each program proves it installs it, MA-1045).
func RefuseUnfinishedDelete(out, logPath string) error {
	unfinishedDeleteMu.RLock()
	check := unfinishedDeleteCheck
	unfinishedDeleteMu.RUnlock()
	if check == nil {
		return nil
	}
	if err := check(logPath, out); err != nil {
		kind := state.FailDeleteUnfinished
		var lu interface{ LogUnreadable() bool }
		if errors.As(err, &lu) && lu.LogUnreadable() {
			kind = state.FailDeletionLogUnreadable
		}
		return withKind(kind, &folderNamedError{err: err, folder: out})
	}
	return nil
}

// UnfinishedDeleteCheckInstalled reports whether a program has installed the engine's
// unfinished-delete check (archdelete does when it loads). Each program's own test asks it, so a
// program that stopped linking archdelete, whose backups RefuseUnfinishedDelete would then let
// through, is caught (the G2 settle pass's LOW-4, MA-1045).
func UnfinishedDeleteCheckInstalled() bool {
	unfinishedDeleteMu.RLock()
	defer unfinishedDeleteMu.RUnlock()
	return unfinishedDeleteCheck != nil
}

// RefusedAsNoArchive reports whether err refused a run because its folder holds no whole archive
// to write into: the folder gone or emptied (archive_absent), or a delete of it that did not finish
// or a deletion log that cannot tell (delete_unfinished, deletion_log_unreadable; MA-1044). A
// scheduled run's callers then take back what they wrote into the folder before the engine's lock
// and tell the refusal outside it (design-archive-delete §7).
func RefusedAsNoArchive(err error) bool {
	switch FailureKind(err) {
	case state.FailArchiveAbsent, state.FailDeleteUnfinished, state.FailDeletionLogUnreadable:
		return true
	}
	return false
}

// folderNamedError is err's words followed by the folder they are about, " (Folder: DIR)", as the
// terminal writes a refusal (for a sink that does not show the folder beside it: a run-job's
// failure log, a scheduled capture's log, an alert email). It unwraps to err.
type folderNamedError struct {
	err    error
	folder string
}

func (e *folderNamedError) Error() string {
	return e.err.Error() + " (Folder: " + util.StripControl(e.folder) + ")"
}

func (e *folderNamedError) Unwrap() error { return e.err }
