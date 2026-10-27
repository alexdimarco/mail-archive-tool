//go:build windows

package util

import "syscall"

// The attributes and reparse tag of a cloud-file placeholder (OneDrive and other
// cloud files providers).
const (
	fileAttributeRecallOnOpen       = 0x00040000
	fileAttributeRecallOnDataAccess = 0x00400000
	reparseTagCloudMask             = 0xFFFF0FFF
	reparseTagCloud                 = 0x9000001A
)

// IsPlaceholder reports whether path is an online-only placeholder: a cloud reparse
// tag together with RECALL_ON_DATA_ACCESS or RECALL_ON_OPEN, read from the entry's
// attributes without opening it (program design R4); never from Go's mode bits or the
// OFFLINE attribute.
func IsPlaceholder(path string) bool {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	var fd syscall.Win32finddata
	h, err := syscall.FindFirstFile(p, &fd)
	if err != nil {
		return false
	}
	syscall.FindClose(h)
	if fd.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 || fd.Reserved0&reparseTagCloudMask != reparseTagCloud {
		return false
	}
	return fd.FileAttributes&(fileAttributeRecallOnOpen|fileAttributeRecallOnDataAccess) != 0
}
