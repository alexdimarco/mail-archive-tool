//go:build windows

package archdelete

import (
	"io/fs"
	"syscall"
)

// The errors removing a file meets, as Windows reports them: a sharing violation
// (32), access denied (5), a device not ready (21).
func inUseErr(rel string) error {
	return &fs.PathError{Op: "remove", Path: rel, Err: syscall.Errno(32)}
}
func deniedErr(rel string) error {
	return &fs.PathError{Op: "remove", Path: rel, Err: syscall.ERROR_ACCESS_DENIED}
}
func goneErr(rel string) error { return &fs.PathError{Op: "remove", Path: rel, Err: syscall.Errno(21)} }
