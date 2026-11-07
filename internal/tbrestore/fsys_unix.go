//go:build !windows

package tbrestore

import (
	"errors"
	"os"
	"syscall"
)

func syncDirFile(f *os.File) error { return f.Sync() }

// isSharingViolation: there is no sharing violation outside Windows; a busy file
// (EBUSY, ETXTBSY) is treated alike.
func isSharingViolation(err error) bool {
	return errors.Is(err, syscall.EBUSY) || errors.Is(err, syscall.ETXTBSY)
}
