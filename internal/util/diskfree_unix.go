//go:build !windows

package util

import (
	"fmt"
	"syscall"
)

func freeBytes(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, fmt.Errorf("free space of %s: %w", dir, err)
	}
	// Bavail is the blocks available to an unprivileged user, quota aware where
	// the filesystem reports it. Its type and Bsize's differ by OS.
	if int64(st.Bavail) < 0 {
		return 0, nil
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
