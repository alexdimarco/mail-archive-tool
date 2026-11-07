//go:build linux

package tbrestore

import (
	"os"
	"syscall"
)

func thunderbirdProcessCount() (int, bool) {
	return countProcThunderbird("/proc", os.Getuid(), statOwner)
}

func statOwner(fi os.FileInfo) (int, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}
