//go:build !windows

package thunderbird

import (
	"os"
	"syscall"
)

// folderIdentity is dir's device and inode, links followed (R6-15); ok false when it
// is not a folder that can be read.
func folderIdentity(dir string) (vol, file uint64, ok bool) {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return 0, 0, false
	}
	st, isStat := fi.Sys().(*syscall.Stat_t)
	if !isStat {
		return 0, 0, false
	}
	return uint64(st.Dev), uint64(st.Ino), true //nolint:unconvert // the types differ by OS
}
