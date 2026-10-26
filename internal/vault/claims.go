package vault

import (
	"errors"
	"io/fs"
)

// KeyFileClaims is what an archive's two key files say WITHOUT the key (A5): whether
// either is there, the archive ids they name — parsed, or merely claimed by a copy
// that does not parse — whether one is from a newer MailArchive, and whether one
// parses. None of it is authenticated; it lets a keyless reader tell "key files
// missing or damaged" from "key files naming another archive" (design phase3 §4.5),
// never trust either.
type KeyFileClaims struct {
	Present bool
	IDs     []ID
	Newer   bool
	Parses  bool
}

// Claims reads both key files in out and reports what they claim. It derives no
// key and opens nothing with one.
func Claims(out string) KeyFileClaims {
	var c KeyFileClaims
	seen := map[ID]bool{}
	for _, name := range []string{FileName, BackupName} {
		cp := readCopy(out, name)
		if errors.Is(cp.err, fs.ErrNotExist) {
			continue
		}
		c.Present = true
		if errors.Is(cp.err, ErrNewerVault) {
			c.Newer = true
		}
		id, ok := ID{}, false
		if cp.v != nil {
			c.Parses = true
			id, ok = cp.v.ArchiveID, true
		} else {
			id, ok = claimedID(cp.raw)
		}
		if ok && !seen[id] {
			seen[id] = true
			c.IDs = append(c.IDs, id)
		}
	}
	return c
}
