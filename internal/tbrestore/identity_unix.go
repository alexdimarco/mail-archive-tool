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
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return Identity{}, errNoIdentity
	}
	return Identity{Volume: uint64(st.Dev), FileID: uint64(st.Ino), FSType: fsType(path)}, nil //nolint:unconvert // the types differ by OS
}
