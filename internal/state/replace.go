package state

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// Windows' answers to a rename whose source or target another process holds
// open without FILE_SHARE_DELETE. golang.org/x/sys/windows names them (a
// Windows test checks they agree); they are spelled out here so the retry is
// exercised on every OS, not only on a Windows host.
const (
	errorAccessDenied     = syscall.Errno(5)  // ERROR_ACCESS_DENIED
	errorSharingViolation = syscall.Errno(32) // ERROR_SHARING_VIOLATION
)

// windowsRenameInUse reports whether err is one of those two answers. It is
// meaningful only for an error Windows returned — on POSIX the same numbers
// are EIO and EPIPE — which is why renameInUse is this on Windows and never
// true elsewhere.
func windowsRenameInUse(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == errorAccessDenied || errno == errorSharingViolation)
}

// renameRetryBudget bounds the total time replaceFile waits between attempts
// (cmd/go's robustio waits up to 2 s for the same Windows behaviour).
const renameRetryBudget = 2 * time.Second

// Test seams: the rename itself and the wait between attempts.
var (
	renameFile  = os.Rename
	renameSleep = time.Sleep
)

// replaceFile renames tmp over dst: the step that makes a fully written,
// fsynced temp file the file of record (R5). On Windows that rename is refused
// for a moment while another process has either file open (renameInUse); a
// failed final save fails the run (F53), so such a moment must not turn a
// scheduled run RED. A refusal renameInUse accepts is retried with a doubling
// wait (1 ms up to 256 ms) until renameRetryBudget of waiting is spent; the
// last error is then returned. Any other error is returned at once, and on
// POSIX nothing is retried.
func replaceFile(tmp, dst string) error {
	delay, waited := time.Millisecond, time.Duration(0)
	for {
		err := renameFile(tmp, dst)
		if err == nil || !renameInUse(err) || waited+delay > renameRetryBudget {
			return err
		}
		renameSleep(delay)
		waited += delay
		if delay < 256*time.Millisecond {
			delay *= 2
		}
	}
}
