//go:build !windows && !darwin

package app

import (
	"os"
)

// On a POSIX system other than macOS the export root and its files are created at
// 0700 and 0600 whatever the umask, and nothing a destination directory carries can
// widen them: a default POSIX ACL's mask is the creation mode's group bits, which
// are zero. So the destination's line is "only you" wherever modes are honoured
// (the filesystem type is checked first, destinationPosture), and the realized check
// reads the created object's own mode.

func osInheritedPosture(dir string) CopyPosture {
	if _, err := os.Stat(dir); err != nil {
		return newPosture(postureKindUnknown, nil)
	}
	return newPosture(postureKindPrivate, nil)
}

func osRealizedPosture(path string, isDir bool) CopyPosture {
	fi, err := os.Lstat(path)
	if err != nil {
		return newPosture(postureKindUnknown, nil)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return newPosture(postureKindReaders, []string{"other accounts on this computer"})
	}
	return newPosture(postureKindPrivate, nil)
}
