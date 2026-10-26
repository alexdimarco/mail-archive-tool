//go:build windows

package util

import (
	"os"
	"syscall"
)

// reparseTagNameSurrogate is the bit a reparse tag carries when the reparse point
// names another place: a symbolic link, a junction, a volume mount point. Cloud-file
// placeholders (OneDrive) carry tags without it.
const reparseTagNameSurrogate = 0x20000000

// IsLinkLike reports whether path itself is a link to somewhere else: on Windows a
// reparse point whose tag is a name surrogate. Go's mode bits alone cannot tell a
// junction from a cloud-file placeholder, so the tag is read. A missing path is not
// one.
func IsLinkLike(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return true, nil
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attrs, err := syscall.GetFileAttributes(p)
	if err != nil {
		return false, err
	}
	if attrs&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return false, nil
	}
	var fd syscall.Win32finddata
	h, err := syscall.FindFirstFile(p, &fd)
	if err != nil {
		return false, err
	}
	syscall.FindClose(h)
	return fd.Reserved0&reparseTagNameSurrogate != 0, nil
}
