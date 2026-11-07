//go:build !windows && !darwin

package tbrestore

import (
	"os"
	"syscall"
)

// makePrivate (Linux and the other Unixes): the entry, opened through the root without
// following a link, is set to 0600 or 0700 and read back as this user's with no group or
// other bit. A POSIX access list's mask takes the group bits, which are then zero, so a
// named entry it carries grants nothing.
func makePrivate(root *os.Root, name string, dir bool) error {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return chmodOwnerOnly(f, dir)
}
