//go:build !windows

package util

import (
	"io/fs"
	"os"
)

// IsLinkLike reports whether path itself is a link to somewhere else: on Unix, a
// symbolic link. A missing path is not one.
func IsLinkLike(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return fi.Mode()&fs.ModeSymlink != 0, nil
}
