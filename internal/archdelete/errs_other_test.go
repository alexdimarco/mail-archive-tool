//go:build !windows

package archdelete

import (
	"io/fs"
	"syscall"
)

// The errors removing a file meets, as this platform reports them: a file a
// program holds, access denied, a drive gone.
func inUseErr(rel string) error  { return &fs.PathError{Op: "remove", Path: rel, Err: syscall.EBUSY} }
func deniedErr(rel string) error { return &fs.PathError{Op: "remove", Path: rel, Err: syscall.EACCES} }
func goneErr(rel string) error   { return &fs.PathError{Op: "remove", Path: rel, Err: syscall.ENXIO} }

// fullErr is a drive with no room left, as this platform reports it.
func fullErr() error { return syscall.ENOSPC }
