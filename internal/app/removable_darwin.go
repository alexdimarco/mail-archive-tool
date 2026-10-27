//go:build darwin

package app

import "golang.org/x/sys/unix"

// removableDrive reads the mount holding abs from statfs: removable by the kernel's
// flag or by where it is mounted (removableMountDarwin). A path statfs cannot read
// is not called removable; the copy's other checks refuse it.
func removableDrive(abs string) bool {
	var sf unix.Statfs_t
	if err := unix.Statfs(abs, &sf); err != nil {
		return false
	}
	return removableMountDarwin(cstr(sf.Mntonname[:]), sf.Flags&unix.MNT_REMOVABLE != 0)
}
