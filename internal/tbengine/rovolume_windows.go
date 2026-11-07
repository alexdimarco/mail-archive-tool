//go:build windows

package tbengine

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// isReadOnlyVolume reports a folder on a read-only volume (GetVolumeInformation's
// FILE_READ_ONLY_VOLUME), where nothing — a backup included — can write the archive
// (R6-13).
func isReadOnlyVolume(dir string) bool {
	root := filepath.VolumeName(dir) + `\`
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return false
	}
	var flags uint32
	if windows.GetVolumeInformation(p, nil, 0, nil, nil, &flags, nil, 0) != nil {
		return false
	}
	return flags&windows.FILE_READ_ONLY_VOLUME != 0
}

// lockDirWritable: Windows has no access(2); Start's own take answers, in words (W-ARCHLOCK).
func lockDirWritable(string) error { return nil }
