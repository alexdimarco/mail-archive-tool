//go:build windows

package lockfile

import (
	"os"

	"golang.org/x/sys/windows"
)

// noFollow: Windows has no O_NOFOLLOW; the Lstat check in Acquire covers it.
const noFollow = 0

// The locked byte range sits far beyond any content, so the holder line at
// offset 0 stays readable by other processes while the lock is held.
const lockOffset = 1 << 62

func tryLock(f *os.File) error {
	ol := new(windows.Overlapped)
	ol.Offset = uint32(lockOffset & 0xFFFFFFFF)
	ol.OffsetHigh = uint32(lockOffset >> 32)
	return windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
}

func unlock(f *os.File) error {
	ol := new(windows.Overlapped)
	ol.Offset = uint32(lockOffset & 0xFFFFFFFF)
	ol.OffsetHigh = uint32(lockOffset >> 32)
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}
