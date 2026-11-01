//go:build !windows

package archdelete

import (
	"errors"
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

// isNotEmpty reports a folder that could not be removed because it holds files.
func isNotEmpty(err error) bool {
	return errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST)
}

// isAccessDenied reports the error Windows gives for a read-only file; never on
// this platform, which never changes a file's mode (§9).
func isAccessDenied(error) bool { return false }

// readOnlyBlocked is Windows' alone: here a file's mode is never changed to remove
// it, so nothing is blocked.
func readOnlyBlocked(string, bool) error { return nil }

// clearReadOnly is Windows' alone (§9: macOS and Linux never get a chmod).
func clearReadOnly(string) error { return errors.New("read-only files are changed only on Windows") }
