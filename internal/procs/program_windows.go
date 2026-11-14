//go:build windows

package procs

import (
	"golang.org/x/sys/windows"
)

// Program is the full path of the program process pid runs (QueryFullProcessImageName),
// so a caller can tell what a recorded pid is now before it acts on it.
func Program(pid int) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:size]), nil
}
