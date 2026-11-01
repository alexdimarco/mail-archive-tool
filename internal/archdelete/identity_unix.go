//go:build !windows

package archdelete

import (
	"errors"
	"os"
	"syscall"
)

// rootIdentity is the folder root is open on: its device and inode.
func rootIdentity(root *os.Root) (Identity, error) {
	f, err := root.Open(".")
	if err != nil {
		return Identity{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return Identity{}, err
	}
	return identityOf(fi)
}

func identityOf(fi os.FileInfo) (Identity, error) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return Identity{}, errors.New("the file system reported no file identity")
	}
	return Identity{Volume: uint64(st.Dev), FileID: uint64(st.Ino)}, nil //nolint:unconvert // the types differ by OS
}

// volumeOf is the volume a path is on, for the drive-root test; ok is false when
// it cannot be read.
func volumeOf(p string) (uint64, bool) {
	fi, err := os.Stat(p)
	if err != nil {
		return 0, false
	}
	id, err := identityOf(fi)
	return id.Volume, err == nil
}
