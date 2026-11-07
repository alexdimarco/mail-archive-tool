//go:build windows

package tbrestore

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// syncDirFile: a folder cannot be flushed on Windows (copyexport_fs.go states it); its
// entries are on disk when the files in it are.
func syncDirFile(*os.File) error { return nil }

// isSharingViolation: antivirus or an indexer holding the file (ERROR_SHARING_VIOLATION,
// ERROR_LOCK_VIOLATION).
func isSharingViolation(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
