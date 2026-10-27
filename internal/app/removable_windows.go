//go:build windows

package app

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// removableDrive asks Windows for the drive type of abs's volume: a removable
// drive (a USB stick or card) or an optical one. An external disk that Windows
// reports as fixed is not caught here — pending the Windows lab (P3-8).
func removableDrive(abs string) bool {
	vol := filepath.VolumeName(abs)
	if vol == "" {
		return false
	}
	rp, err := windows.UTF16PtrFromString(vol + `\`)
	if err != nil {
		return false
	}
	switch windows.GetDriveType(rp) {
	case windows.DRIVE_REMOVABLE, windows.DRIVE_CDROM:
		return true
	}
	return false
}
