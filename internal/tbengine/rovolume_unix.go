//go:build darwin || linux

package tbengine

import "golang.org/x/sys/unix"

// isReadOnlyVolume reports a folder on a volume mounted read-only (statfs: MNT_RDONLY
// on macOS, ST_RDONLY on Linux), where nothing — a backup included — can write the
// archive (R6-13).
func isReadOnlyVolume(dir string) bool {
	var st unix.Statfs_t
	if unix.Statfs(dir, &st) != nil {
		return false
	}
	return uint64(st.Flags)&uint64(rdonlyFlag) != 0 //nolint:unconvert // the types differ by OS
}

// lockDirWritable is whether this account may make a file in dir (access(2), W_OK and
// X_OK), asked without making one.
func lockDirWritable(dir string) error { return unix.Access(dir, unix.W_OK|unix.X_OK) }
