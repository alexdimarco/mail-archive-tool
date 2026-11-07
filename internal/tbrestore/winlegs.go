package tbrestore

import (
	"os"
	"path/filepath"
	"strings"
)

// The Windows legs of the Thunderbird side (design §3.1 step 1 note, §3.2 step 4 note;
// MA-1143). The rules are written over plain paths so every platform builds them; only
// Windows calls them with its own folders (winlegs_windows.go).

// storePackageIn reports whether packagesDir (the local application-data Packages folder) holds a folder
// directly inside it whose name contains "Thunderbird", case-blind: a Thunderbird from
// the Microsoft Store may keep its profiles elsewhere, so the plan warns (W-STORE) until
// lab row MA-1097 says where (lead's call: warn, do not refuse).
func storePackageIn(packagesDir string) bool {
	entries, err := os.ReadDir(packagesDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && strings.Contains(strings.ToLower(e.Name()), "thunderbird") {
			return true
		}
	}
	return false
}

// isUNC reports a path on a network share by its spelling (\\server\share\…), as
// Windows writes one; \\?\ and \\.\ device paths are not shares by themselves.
func isUNC(p string) bool {
	if !strings.HasPrefix(p, `\\`) {
		return false
	}
	if strings.HasPrefix(p, `\\?\UNC\`) {
		return true
	}
	return !strings.HasPrefix(p, `\\?\`) && !strings.HasPrefix(p, `\\.\`)
}

// volumeRoot is the drive root of a Windows path ("C:\"), "" when it has none.
func volumeRoot(p string) string {
	v := filepath.VolumeName(p)
	if len(v) == 2 && v[1] == ':' {
		return v + `\`
	}
	return ""
}

// StoreWarning is W-STORE when this computer may run a Microsoft Store Thunderbird
// (Windows only; "" elsewhere). The plan shows it first, and the Done words repeat it.
func StoreWarning() string {
	if storeInstallFound() {
		return wStore
	}
	return ""
}

// NetworkWarning is W-NETWORK for a Local Folders on a network share whose identity
// could be read (Windows only; "" elsewhere): the restore may go ahead (lead's call L2).
func NetworkWarning(lf LocalFolders) string {
	if onNetwork(lf.Dir) {
		return fill(wNetwork, "PATH", lf.Dir)
	}
	return ""
}
