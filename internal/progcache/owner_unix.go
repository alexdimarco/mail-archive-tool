//go:build !windows

package progcache

import (
	"os"
	"syscall"
)

// ownedByMe says this user owns the file.
func ownedByMe(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Geteuid()
}
