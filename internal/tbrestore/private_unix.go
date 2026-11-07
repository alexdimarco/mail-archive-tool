//go:build !windows

package tbrestore

import (
	"os"
	"syscall"
)

// chmodOwnerOnly sets f's mode to 0600 (0700 for a folder) and reads it back as this
// user's with no group or other bit (R6-01).
func chmodOwnerOnly(f *os.File, dir bool) error {
	mode := os.FileMode(0o600)
	if dir {
		mode = 0o700
	}
	if err := f.Chmod(mode); err != nil {
		return err
	}
	return ownerOnlyMode(f)
}

// ownerOnlyMode reads f back: this user's, no group or other bit.
func ownerOnlyMode(f *os.File) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if fi.Mode().Perm()&0o077 != 0 || !ok || int(st.Uid) != os.Getuid() {
		return errNotPrivate
	}
	return nil
}
