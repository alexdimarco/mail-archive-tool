//go:build !unix

package source

import "io/fs"

// dirIdentity: where the file system's identity of a directory is not in a FileInfo (on
// Windows it is the file index, which only an open handle reads), the visited set is a
// list compared with os.SameFile.
func dirIdentity(fi fs.FileInfo) (id [2]uint64, ok bool) { return id, false }
