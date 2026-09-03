//go:build windows

// Package procs answers one question the status surfaces need: is a recorded
// pid still alive?
package procs

import (
	"golang.org/x/sys/windows"
)

// Alive reports whether a process with pid exists and has not exited.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}
