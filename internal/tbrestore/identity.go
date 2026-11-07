package tbrestore

import "errors"

// Identity is a folder's identity: its volume and file id (device and inode on
// macOS and Linux; volume serial number and file index on Windows), and the name of
// the volume's file system when it can be read ("NTFS", "FAT32", "apfs", "msdos").
// A profile list entry and a Local Folders are pinned by it between the plan and the
// start, so a folder swapped in between is refused (design §3.1 step 6, §3.2 step 4).
type Identity struct {
	Volume uint64 `json:"volume"`
	FileID uint64 `json:"file_id"`
	FSType string `json:"fs,omitempty"`
}

// Same reports whether two identities name one folder (the file system name aside).
func (a Identity) Same(b Identity) bool { return a.Volume == b.Volume && a.FileID == b.FileID }

// errNoIdentity is a volume that reports no file identity (some network shares on
// Windows): W-NOIDENT.
var errNoIdentity = errors.New("the volume reported no file identity")

// dirIdentitySeam reads a folder's identity; a test replaces it to give a volume
// that reports none (MA-1143).
var dirIdentitySeam = dirIdentity

// DirIdentity reads the identity of the folder at path (links followed: the caller
// resolves them first and shows the resolved path).
func DirIdentity(path string) (Identity, error) { return dirIdentitySeam(path) }
