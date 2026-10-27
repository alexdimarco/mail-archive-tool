//go:build darwin

package util

import "golang.org/x/sys/unix"

// sfDataless is SF_DATALESS: the file's bytes are not on this disk (an iCloud or
// File Provider placeholder) and reading it fetches them.
const sfDataless = 0x40000000

// IsPlaceholder reports whether path is an online-only placeholder, read from the
// file's flags without opening it (program design R4); never from Go's mode bits.
func IsPlaceholder(path string) bool {
	var st unix.Stat_t
	if unix.Lstat(path, &st) != nil {
		return false
	}
	return st.Flags&sfDataless != 0
}
