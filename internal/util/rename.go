package util

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// Windows' answers to a rename whose source or target another process holds
// open without FILE_SHARE_DELETE. golang.org/x/sys/windows names them (a
// Windows test in internal/state checks they agree); they are spelled out here
// so the retry is exercised on every OS, not only on a Windows host.
const (
	ErrorAccessDenied     = syscall.Errno(5)  // ERROR_ACCESS_DENIED
	ErrorSharingViolation = syscall.Errno(32) // ERROR_SHARING_VIOLATION
)

// WindowsRenameInUse reports whether err is one of those two answers. It is
// meaningful only for an error Windows returned — on POSIX the same numbers
// are EIO and EPIPE — which is why a caller's in-use rule is this on Windows
// and never true elsewhere.
func WindowsRenameInUse(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == ErrorAccessDenied || errno == ErrorSharingViolation)
}

// RenameRetryBudget bounds the total time ReplaceRetrying waits between
// attempts (cmd/go's robustio waits up to 2 s for the same Windows behaviour).
const RenameRetryBudget = 2 * time.Second

// ReplaceRetrying renames tmp over dst with rename, waiting out (with sleep) a
// refusal inUse accepts — on Windows, another process briefly holding either
// file open — with a doubling wait (1 ms up to 256 ms) until RenameRetryBudget
// of waiting is spent; the last error is then returned. Any other error is
// returned at once. The three functions are the caller's seams.
func ReplaceRetrying(tmp, dst string, rename func(string, string) error, inUse func(error) bool, sleep func(time.Duration)) error {
	delay, waited := time.Millisecond, time.Duration(0)
	for {
		err := rename(tmp, dst)
		if err == nil || !inUse(err) || waited+delay > RenameRetryBudget {
			return err
		}
		sleep(delay)
		waited += delay
		if delay < 256*time.Millisecond {
			delay *= 2
		}
	}
}

// Seams of this package's own atomic writer: the rename and the wait between
// attempts (renameInUse is per-OS, in rename_windows.go / rename_other.go).
var (
	renameFile  = os.Rename
	renameSleep = time.Sleep
)
