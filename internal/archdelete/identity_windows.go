//go:build windows

package archdelete

import (
	"errors"
	"os"
	"syscall"
)

// rootIdentity is the folder root is open on: its volume serial number and file
// index, read from the open handle (FILE_FLAG_BACKUP_SEMANTICS, as os opens a
// folder), so no second lookup of the path can name another folder.
func rootIdentity(root *os.Root) (Identity, error) {
	f, err := root.Open(".")
	if err != nil {
		return Identity{}, err
	}
	defer f.Close()
	return handleIdentity(f)
}

func handleIdentity(f *os.File) (Identity, error) {
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &info); err != nil {
		return Identity{}, err
	}
	id := Identity{Volume: uint64(info.VolumeSerialNumber), FileID: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)}
	if id.Volume == 0 && id.FileID == 0 {
		return id, errors.New("the volume reported no file identity")
	}
	return id, nil
}

// volumeOf is the volume a path is on, for the drive-root test; ok is false when
// it cannot be read.
func volumeOf(p string) (uint64, bool) {
	f, err := os.Open(p)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	id, err := handleIdentity(f)
	return id.Volume, err == nil
}
