//go:build windows

package app

import (
	"os"

	"golang.org/x/sys/windows"
)

// fileLinks is the number of hard links to the open file f (nNumberOfLinks of
// GetFileInformationByHandle): a regular file with more than one is refused by the
// archive gate, because a hard link defeats a component-wise Lstat walk and os.Root alike
// (design §4.6, XC19).
func fileLinks(f *os.File) (uint64, error) {
	var n uint64
	var ferr error
	if err := rawControl(f, func(h windows.Handle) {
		var info windows.ByHandleFileInformation
		if e := windows.GetFileInformationByHandle(h, &info); e != nil {
			ferr = e
			return
		}
		n = uint64(info.NumberOfLinks)
	}); err != nil {
		return 0, err
	}
	return n, ferr
}
