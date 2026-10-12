//go:build unix

package source

import (
	"io/fs"
	"syscall"
)

// dirIdentity is a directory's identity as the file system gives it, a device and an
// inode: what the opt-in's visited set is keyed by. ok is false where it cannot be read
// from a FileInfo.
func dirIdentity(fi fs.FileInfo) (id [2]uint64, ok bool) {
	st, isStat := fi.Sys().(*syscall.Stat_t)
	if !isStat {
		return id, false
	}
	return [2]uint64{uint64(st.Dev), uint64(st.Ino)}, true //nolint:unconvert // the types differ by OS
}
