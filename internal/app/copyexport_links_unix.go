//go:build !windows

package app

import (
	"os"
	"syscall"
)

// fileLinks is the number of hard links to the open file f (its st_nlink): a regular file
// with more than one is refused by the archive gate, because a hard link defeats a
// component-wise Lstat walk and os.Root alike (design §4.6, XC19). 0 means the OS did not
// say.
func fileLinks(f *os.File) (uint64, error) {
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink), nil
	}
	return 0, nil
}
