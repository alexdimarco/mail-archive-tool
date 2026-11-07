//go:build windows

package tbrestore

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// storeInstallFound looks in the Packages folder of the local application-data folder, read from
// FOLDERID_LocalAppData, never the environment variable.
func storeInstallFound() bool {
	p, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil || p == "" {
		return false
	}
	return storePackageIn(filepath.Join(p, "Packages"))
}

// driveTypeSeam is GetDriveTypeW; a test replaces it to give a remote drive.
var driveTypeSeam = func(root string) uint32 {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return windows.DRIVE_UNKNOWN
	}
	return windows.GetDriveType(p)
}

// onNetwork: a UNC path, or a drive whose GetDriveTypeW is DRIVE_REMOTE.
func onNetwork(p string) bool {
	if isUNC(p) {
		return true
	}
	if root := volumeRoot(p); root != "" {
		return driveTypeSeam(root) == windows.DRIVE_REMOTE
	}
	return false
}
