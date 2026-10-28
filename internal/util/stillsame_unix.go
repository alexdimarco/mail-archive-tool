//go:build darwin || linux

package util

import (
	"os"
	"syscall"
)

// changeTimeSame compares the two files' change times, when both carry them.
func changeTimeSame(a, b os.FileInfo) bool {
	sa, ok1 := a.Sys().(*syscall.Stat_t)
	sb, ok2 := b.Sys().(*syscall.Stat_t)
	if !ok1 || !ok2 {
		return true
	}
	return statCtime(sa) == statCtime(sb)
}
