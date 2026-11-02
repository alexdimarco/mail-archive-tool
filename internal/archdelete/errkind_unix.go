//go:build !windows

package archdelete

import (
	"errors"
	"os"
	"syscall"
)

// causeOf is the kind of error that kept a file, for its remedy (§8 (a)).
func causeOf(err error) Cause {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return CauseOther
	}
	switch errno {
	case syscall.EBUSY, syscall.ETXTBSY:
		return CauseInUse
	case syscall.EACCES, syscall.EPERM, syscall.EROFS:
		return CauseDenied
	case syscall.ENXIO, syscall.ENODEV, syscall.EIO, syscall.ENOTCONN, syscall.ESTALE:
		return CauseDeviceGone
	}
	return CauseOther
}

// heldIn is Windows' alone: here a file another program has open can still be deleted.
func heldIn(*os.Root, string) bool { return false }

// isNotEmpty reports a folder that could not be removed because it holds files.
func isNotEmpty(err error) bool {
	return errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST)
}

// isAccessDenied reports the error Windows gives for a read-only file; never on
// this platform, which never changes a file's mode (§9).
func isAccessDenied(error) bool { return false }

// readOnlyBlocked is Windows' alone: here a file's mode is never changed to remove
// it, so nothing is blocked.
func readOnlyBlocked(*os.Root, string, bool) error { return nil }

// clearReadOnly is Windows' alone (§9: macOS and Linux never get a chmod).
func clearReadOnly(*os.Root, string) error {
	return errors.New("read-only files are changed only on Windows")
}

// deniedRemedy is §8 (a)'s remedy for a permission refused on macOS and Linux: a
// file locked in the Finder, a folder that may not be changed, a read-only drive —
// never Windows' words (del-s1b-A3-08).
const deniedRemedy = "The system did not allow it (a locked file, a folder you may not change, or a read-only drive): unlock it and click Delete again, or delete these by hand."

// deniedNamesFolders: here removing a file needs leave to change the folder holding it, so the
// remedy names the folders of the files refused (the 1.5.3 friction review's B6, wording item 24).
const deniedNamesFolders = true
