package archdelete

import (
	"path/filepath"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/lockfile"
)

// msgBackupDeleting is a backup's refusal while a delete of its folder is still running: it holds
// the archive's lock, so the delete has not stopped part way and "click Delete again" would be
// wrong (the G2 settle pass's LOW-1, MA-1045; builder's words, after msgDeleting).
const msgBackupDeleting = "This archive is being deleted by another MailArchive window or terminal, so it was not backed up; nothing was changed. When that delete ends, see its result there."

// BackupRefusal is why a backup does not run into folder because a delete of it did not finish
// (MA-1044; the 1.5.3 re-walk's guards pass, G2): the log at logPath ("" is MailArchive's own
// deletions.log) holds a started line for it with no finished line, so the run would write into a
// folder the delete stopped part way, and the emptied-folder guard lets it in while the manifest,
// removed last, is there. The words are MA-1015's ActionRefusal gives, "Delete not finished: click
// Delete again. This archive's delete did not finish, so it was not backed up; nothing was
// changed."; a log that cannot be read refuses in its words too. A delete that is still running
// (it holds the folder's lock, its holder line naming a delete) is not one that did not finish: it
// refuses in msgBackupDeleting's words, as a held lock (CaseBusy, matching lockfile.ErrHeld), so a
// scheduled run skips quietly as it did before this check (app.ScheduledLockSkip; MA-1045). A
// delete's own re-run (Delete again) is not a backup and never asks it. nil means nothing stops the
// backup.
func BackupRefusal(logPath, folder string) error {
	err := ActionRefusal(logPath, folder, "it was not backed up")
	if r := AsRefusal(err); r != nil && r.Case == CaseUnfinished && deleteRunning(folder) {
		return &deletingRefusal{r: &Refusal{Case: CaseBusy, Message: msgBackupDeleting}}
	}
	return err
}

// deleteRunning reports whether a delete holds folder's archive lock now: the lock is held (probed
// without creating or writing the file) and its holder line names a delete.
func deleteRunning(folder string) bool {
	lock := filepath.Join(folder, lockName)
	return lockfile.Held(lock) && lockfile.HolderVerb(lock) == "delete"
}

// deletingRefusal is msgBackupDeleting's refusal: a *Refusal (CaseBusy) that also matches
// lockfile.ErrHeld, so app.FailureKind names it archive_busy and app.ScheduledLockSkip treats it
// as the held lock it is.
type deletingRefusal struct{ r *Refusal }

func (e *deletingRefusal) Error() string   { return e.r.Message }
func (e *deletingRefusal) Unwrap() []error { return []error{e.r, lockfile.ErrHeld} }

// LogUnreadable reports whether r refused because deletions.log could not be read (or MailArchive's
// settings folder, which holds it, could not be found), so app.RefuseUnfinishedDelete names the
// refusal's kind deletion_log_unreadable (MA-1045).
func (r *Refusal) LogUnreadable() bool { return r.Case == CaseLogUnreadable }

// The engine (app.Run, app.RunGraph) asks BackupRefusal at a run's start and again under its lock,
// through app.RefuseUnfinishedDelete: package app cannot import this one, so it is installed here,
// when any program that runs a backup loads this package (each program's test proves it does,
// app.UnfinishedDeleteCheckInstalled; MA-1045).
func init() {
	app.SetUnfinishedDeleteCheck(BackupRefusal)
}
