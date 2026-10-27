//go:build windows

package util

import "syscall"

// IsHydratedCloudFile reports a cloud file (OneDrive and other cloud files providers)
// whose bytes are on this disk: a cloud reparse tag without RECALL_ON_DATA_ACCESS or
// RECALL_ON_OPEN. Go's mode bits call it irregular; reading it recalls nothing, so it
// is judged as a regular file (program design R4, the PG pass's CRIT-01).
func IsHydratedCloudFile(path string) bool {
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
	if fd.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 || fd.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 ||
		fd.Reserved0&reparseTagCloudMask != reparseTagCloud {
		return false
	}
	return fd.FileAttributes&(fileAttributeRecallOnOpen|fileAttributeRecallOnDataAccess) == 0
}
