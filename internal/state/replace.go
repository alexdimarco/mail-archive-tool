package state

import (
	"os"
	"time"

	"mail-archive-tool/internal/util"
)

// Windows' answers to a rename whose source or target another process holds
// open, and the rule that recognises them: shared with util's atomic writer.
const (
	errorAccessDenied     = util.ErrorAccessDenied
	errorSharingViolation = util.ErrorSharingViolation
)

var windowsRenameInUse = util.WindowsRenameInUse

// renameRetryBudget bounds the total time replaceFile waits between attempts.
const renameRetryBudget = util.RenameRetryBudget

// Test seams: the rename itself and the wait between attempts.
var (
	renameFile  = os.Rename
	renameSleep = time.Sleep
)

// replaceFile renames tmp over dst: the step that makes a fully written temp
// file the file of record (R5). Every file this package replaces by rename goes
// through it: the manifest, the last-run and last-verify records, the attention
// and integrity sidecars, and the compacted history log. On Windows that rename
// is refused for a moment while another process has either file open
// (renameInUse); a failed final manifest save fails the run (F53), and a lost
// "ok" last-run record leaves `status` reading a run that never finished, so
// such a moment must not turn a scheduled run RED. A refusal renameInUse
// accepts is retried with a doubling wait (1 ms up to 256 ms) until
// renameRetryBudget of waiting is spent; the last error is then returned. Any
// other error is returned at once, and on POSIX nothing is retried.
func replaceFile(tmp, dst string) error {
	return util.ReplaceRetrying(tmp, dst, renameFile, renameInUse, renameSleep)
}
