//go:build windows

package tbrestore

import (
	"golang.org/x/sys/windows"
)

// lockHelper is the child's side on Windows: parent.lock opened as Thunderbird opens
// it (read/write, no sharing, CREATE_ALWAYS is Thunderbird's; the helper uses
// OPEN_ALWAYS so it never truncates). "getlk" answers held when that open fails,
// free when it succeeds (and closes it); "take" and "hold" answer took or refused.
func lockHelper(mode, path string) string {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "error " + err.Error()
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	switch mode {
	case "getlk":
		if err != nil {
			return "held"
		}
		windows.CloseHandle(h)
		return "free"
	case "take", "hold":
		if err != nil {
			return "refused"
		}
		if mode == "take" {
			windows.CloseHandle(h)
		}
		return "took"
	}
	return "unknown mode"
}
