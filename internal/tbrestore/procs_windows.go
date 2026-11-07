//go:build windows

package tbrestore

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// thunderbirdProcessCount counts every thunderbird.exe on the machine, for every
// user (presync's Windows probe matches the same way, env_windows.go; design §5.1
// note: the safe direction). A snapshot that cannot be taken is not known.
func thunderbirdProcessCount() (int, bool) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	if err := windows.Process32First(snap, &e); err != nil {
		return 0, false
	}
	n := 0
	for {
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), "thunderbird.exe") {
			n++
		}
		if err := windows.Process32Next(snap, &e); err != nil {
			break
		}
	}
	return n, true
}
