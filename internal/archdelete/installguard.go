package archdelete

import "mail-archive-tool/internal/util"

// msgInstallUndoFailed is GuardedInstall's refusal when a delete of the folder began while its
// entry went in and the entry could not be removed again: it may run, and the delete's own step 2
// stops it (builder's words, for the operator). The first %s is what was installed ("its
// schedule"), the second why the removal failed.
const msgInstallUndoFailed = "Delete not finished: click Delete again. A delete of this archive began while %s was being installed, and that schedule could not be removed again (%s), so it may still run: click Delete again, which stops it."

// GuardedInstall installs a scheduled entry for folder only while no delete of it is unfinished
// (MA-1036; the 1.5.3 re-walk's guards pass, G1 and G4): every installer outside the Desktop — the
// terminal's `schedule -install`, the wizard's repair (and so Resume) and its "Yes — every day"
// offer (and so the Desktop's Add another archive) — goes through it. A scheduled backup put back
// for a folder whose delete stopped part way would write into it, and its run passes the
// emptied-folder guard, since the manifest goes last.
//
// It asks deletions.log at logPath ("" is MailArchive's own) before install, refusing in
// MA-1015's words ActionRefusal gives ("…so WHAT was not installed; nothing was changed."), with
// nothing installed. It asks again after install: a delete that began while the entry went in may
// have run its step 2, which stops the schedules, before the entry existed, so undo removes it
// again ("…so WHAT was installed and then removed again…"); a log that cannot be read then is
// treated the same way, since whether that happened cannot be told. A delete that begins after the
// second ask finds the entry, and its step 2 stops it. what names the entry ("its schedule", "its
// scheduled backup"). An install's own error is returned as it is.
func GuardedInstall(logPath, folder, what string, install, undo func() error) error {
	if err := ActionRefusal(logPath, folder, what+" was not installed"); err != nil {
		return err
	}
	if err := install(); err != nil {
		return err
	}
	if err := ActionRefusal(logPath, folder, what+" was installed and then removed again"); err != nil {
		if uerr := undo(); uerr != nil {
			return refuse(CaseUnfinished, msgInstallUndoFailed, what, util.StripControl(uerr.Error()))
		}
		return err
	}
	return nil
}
