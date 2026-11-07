//go:build windows

package tbrestore

import (
	"os"

	"golang.org/x/sys/windows"
)

// dirIdentity reads the folder's volume serial number and file index from an open
// handle (os.Open opens a folder with FILE_FLAG_BACKUP_SEMANTICS), and its volume's
// file system name. A volume that reports neither is errNoIdentity (W-NOIDENT).
func dirIdentity(path string) (Identity, error) {
	f, err := os.Open(path)
	if err != nil {
		return Identity{}, err
	}
	defer f.Close()
	h := windows.Handle(f.Fd())
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return Identity{}, err
	}
	id := Identity{Volume: uint64(info.VolumeSerialNumber), FileID: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)}
	if id.Volume == 0 && id.FileID == 0 {
		return id, errNoIdentity
	}
	fsName := make([]uint16, 64)
	if windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, nil, &fsName[0], uint32(len(fsName))) == nil {
		id.FSType = windows.UTF16ToString(fsName)
	}
	return id, nil
}
