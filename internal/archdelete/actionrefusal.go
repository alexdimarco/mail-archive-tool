package archdelete

import (
	"runtime"

	"mail-archive-tool/internal/util"
)

// CaseLogUnreadable is an action on a folder refused because deletions.log cannot be read, so
// whether a delete of the folder finished cannot be told (MA-1015).
const CaseLogUnreadable Case = "log-unreadable"

// The words of MA-1015 (MA-987's "Delete not finished: click Delete again" first; the rest the
// builder's, for the operator). %s in msgActionUnfinished says what was not done ("it was not
// paused"); msgActionLogUnreadable names the log and why it could not be read.
const (
	msgActionUnfinished    = "Delete not finished: click Delete again. This archive's delete did not finish, so %s; nothing was changed."
	msgActionLogUnreadable = "MailArchive could not read its deletion log, %s (%s), so it cannot tell whether a delete of this archive did not finish; nothing was changed. Check that file, then try again."
	// msgActionNoSettings is the refusal when MailArchive's settings folder, and so its deletion
	// log's path, cannot be found (no home folder set): there is no file to name (the re-walk's
	// guards pass, G7; builder's words).
	msgActionNoSettings = "MailArchive could not find its settings folder, which holds its deletion log (%s), so it cannot tell whether a delete of this archive did not finish; nothing was changed. Check that your home folder is set (HOME, or AppData on Windows), then try again."
)

// ActionRefusal is why Pause, Resume or a Switch does not act on folder because a delete of it
// did not finish (§5.4; MA-1015, the 1.5.3 friction review's H2 and its re-walk's F1): the log at
// logPath holds a started line for it with no finished line, and Pause would write its marker into
// the half-deleted folder, Resume put back the scheduled backup the delete stopped (whose next run
// passes the emptied-folder guard, since the manifest goes last), and a Switch make it the current
// archive again. done says what was not done. A log that cannot be read refuses (CaseLogUnreadable,
// naming it), since whether the delete finished cannot be told. An empty logPath is MailArchive's
// own deletions.log (DefaultLogPath), so no caller skips the check by passing none. nil means
// nothing stops the action.
func ActionRefusal(logPath, folder, done string) error {
	if logPath == "" {
		p, err := DefaultLogPath()
		if err != nil {
			return refuse(CaseLogUnreadable, msgActionNoSettings, util.StripControl(err.Error()))
		}
		logPath = p
	}
	u, err := UnfinishedDelete(logPath, folder, runtime.GOOS)
	if err != nil {
		return refuse(CaseLogUnreadable, msgActionLogUnreadable, logPath, util.StripControl(err.Error()))
	}
	if u != nil {
		return refuse(CaseUnfinished, msgActionUnfinished, done)
	}
	return nil
}
