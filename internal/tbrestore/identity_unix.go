//go:build !windows

package tbrestore

import (
	"os"
	"syscall"
)

func dirIdentity(path string) (Identity, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return Identity{}, err
	}
	id, err := infoIdentity(fi)
	if err != nil {
		return Identity{}, err
	}
	id.FSType = fsType(path)
	return id, nil
}

// openIdentity is the identity of an open file or folder.
func openIdentity(f *os.File) (Identity, error) {
	fi, err := f.Stat()
	if err != nil {
		return Identity{}, err
	}
	return infoIdentity(fi)
}

func infoIdentity(fi os.FileInfo) (Identity, error) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return Identity{}, errNoIdentity
	}
	return Identity{Volume: uint64(st.Dev), FileID: uint64(st.Ino)}, nil //nolint:unconvert // the types differ by OS
}
