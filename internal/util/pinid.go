package util

import "os"

// PinIdentity fixes fi's file identity now and returns fi. On Windows a FileInfo
// from os.Stat or os.Lstat loads the file's volume and index only when os.SameFile
// first asks for them — by opening the path again then — so an identity taken early
// and compared later would be the later file's: a file swapped meanwhile would match
// itself (P3-8 Windows lab, B1). Every FileInfo kept to be compared later with
// os.SameFile is pinned where it is taken. Elsewhere the identity is fixed already
// and this does nothing.
func PinIdentity(fi os.FileInfo) os.FileInfo {
	if fi != nil {
		os.SameFile(fi, fi)
	}
	return fi
}
