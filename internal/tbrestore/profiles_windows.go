//go:build windows

package tbrestore

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// profileBases: %APPDATA%\Thunderbird, %APPDATA% from FOLDERID_RoamingAppData, never
// the environment variable (design §3.1 step 1).
func profileBases() []string {
	p, err := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, 0)
	if err != nil || p == "" {
		return nil
	}
	return []string{filepath.Join(p, "Thunderbird")}
}

// profileModeOK: ownership and mode are not checked on Windows, as presync states
// (resolve_windows.go): there is no portable per-file check, and the profile's parent,
// %APPDATA%, is the user's own (design §9 item 4).
func profileModeOK(os.FileInfo) bool { return true }
