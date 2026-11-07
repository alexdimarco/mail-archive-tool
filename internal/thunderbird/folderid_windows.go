//go:build windows

package thunderbird

import (
	"os"

	"golang.org/x/sys/windows"
)

// folderIdentity is dir's volume serial number and file index, read from a handle
// (os.Open opens a folder with FILE_FLAG_BACKUP_SEMANTICS, through junctions and subst
// drives alike) (R6-15); ok false when it is not a folder that can be read or its volume
// reports no identity.
func folderIdentity(dir string) (vol, file uint64, ok bool) {
	f, err := os.Open(dir)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.IsDir() {
		return 0, 0, false
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return 0, 0, false
	}
	vol, file = uint64(info.VolumeSerialNumber), uint64(info.FileIndexHigh)<<32|uint64(info.FileIndexLow)
	return vol, file, vol != 0 || file != 0
}
