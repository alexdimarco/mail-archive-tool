package app

import (
	"errors"
	"io/fs"
	"os"
	"strings"
)

// ceIdentity is the identity tuple of an export root (design §4.9, RR2-C4): what the
// run reads from the directory right after it creates it and records in its run
// record, so a later launch can tell this folder from another at the same path.
// POSIX numbers dev and inode by the mount, and some filesystems (vfat, exfat,
// msdos) number inodes afresh each time they are loaded, so the run id the marker
// and README carry (the content nonce) is the first test, and dev+inode decides
// only where it is stable.
type ceIdentity struct {
	FSType string `json:"fs,omitempty"`     // the filesystem's type name, as the OS reports it
	Dev    uint64 `json:"dev,omitempty"`    // POSIX st_dev
	Ino    uint64 `json:"ino,omitempty"`    // POSIX st_ino
	Vol    uint32 `json:"vol,omitempty"`    // Windows volume serial number
	FileID uint64 `json:"fileid,omitempty"` // Windows file ID
	Birth  int64  `json:"birth,omitempty"`  // the directory's birth time (unix ns) where the filesystem reports one: statx btime, st_birthtime
}

// isWindowsID reports whether the tuple is a Windows one.
func (i ceIdentity) isWindowsID() bool { return i.Vol != 0 || i.FileID != 0 }

// stableInodes reports whether a filesystem type numbers its inodes stably across a
// remount: not vfat, exfat or msdos, on any OS (design §4.9 rule 3).
func stableInodes(fsType string) bool {
	switch strings.ToLower(fsType) {
	case "vfat", "msdos", "msdosfs", "exfat", "fat", "fat32", "fuseblk", "":
		return false
	}
	return true
}

// idVerdict is what an identity comparison decides.
type idVerdict int

const (
	// idConfirmed: this is the folder the record names.
	idConfirmed idVerdict = iota
	// idMismatch is positive: another run id, a non-directory, a symlink or reparse
	// point, a Windows identity mismatch, or a new inode on the same mount of a
	// stable-inode filesystem. "Moved or replaced" (R-r).
	idMismatch
	// idUnconfirmed: neither confirmed nor positively different. "Couldn't confirm"
	// (R-r2): a replugged FAT stick, a remount, a folder with no nonce and no file
	// identity that can decide.
	idUnconfirmed
)

func (v idVerdict) String() string {
	switch v {
	case idConfirmed:
		return "confirmed"
	case idMismatch:
		return "moved or replaced"
	}
	return "couldn't confirm"
}

// ceFolderSeen is what a probe of an export folder saw.
type ceFolderSeen struct {
	Exists    bool
	IsDir     bool
	IsLink    bool       // a symlink or a reparse point
	Nonce     string     // the run id the marker, or else README's first line, carries; "" when neither is there
	HasMarker bool       // the marker file is present
	HasReadme bool       // README.txt is present
	Cur       ceIdentity // the folder's identity as it reads now
}

// judgeIdentity applies design §4.9's tuple: the record's run id and identity
// against what the probe saw.
func judgeIdentity(runID string, rec ceIdentity, seen ceFolderSeen) (idVerdict, string) {
	switch {
	case !seen.IsDir:
		return idMismatch, "it is not a folder"
	case seen.IsLink:
		return idMismatch, "it is a link"
	}
	cur := seen.Cur
	if seen.Nonce != "" {
		if seen.Nonce != runID {
			return idMismatch, "it holds another export's run id"
		}
		// Rule 1 holds. A Windows identity must also match (rule 2); a POSIX one is
		// compared only where inodes are stable and dev has not changed (rule 3).
		if rec.isWindowsID() {
			if cur.Vol != rec.Vol || cur.FileID != rec.FileID {
				return idMismatch, "its Windows file identity differs"
			}
			return idConfirmed, ""
		}
		if stableInodes(rec.FSType) && cur.Dev == rec.Dev && cur.Ino != rec.Ino {
			return idMismatch, "a different folder is at that place now"
		}
		return idConfirmed, ""
	}
	// No nonce: an empty or temp-only root from a W2-W4 kill, or a root whose marker
	// is gone and that has no README. Its file identity alone decides, where it can.
	if rec.isWindowsID() {
		if cur.Vol == rec.Vol && cur.FileID == rec.FileID {
			return idConfirmed, ""
		}
		return idMismatch, "its Windows file identity differs"
	}
	if stableInodes(rec.FSType) && cur.Dev == rec.Dev {
		if cur.Ino != rec.Ino {
			return idMismatch, "a different folder is at that place now"
		}
		// The same inode on ext4 or xfs may be a folder someone recreated at the same
		// path (a freed inode number is reused, RR2b-N1), so the directory's birth
		// time, recorded at mkdir, must match too; a filesystem that reports none
		// cannot confirm it.
		if rec.Birth == 0 || cur.Birth == 0 {
			return idUnconfirmed, "this filesystem keeps no creation time to tell it from a folder made again"
		}
		if cur.Birth != rec.Birth {
			return idMismatch, "it was created again after the export"
		}
		return idConfirmed, ""
	}
	return idUnconfirmed, "the drive may have been plugged in again"
}

// probeFolder looks at an export folder through nothing but the path: it reads the
// presence, the marker, README's first line and the identity, never status, ids or
// hashes (XC4). A probe error other than "not there" is returned for the caller to
// word ("did not answer").
func probeFolder(path string) (ceFolderSeen, error) {
	var seen ceFolderSeen
	fi, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return seen, nil
		}
		return seen, err
	}
	seen.Exists = true
	seen.IsLink = fi.Mode()&fs.ModeSymlink != 0 || isReparsePoint(fi)
	seen.IsDir = fi.IsDir()
	if !seen.IsDir || seen.IsLink {
		return seen, nil
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return seen, err
	}
	defer root.Close()
	if f, err := root.Open("."); err == nil {
		seen.Cur, _ = readIdentity(f)
		f.Close()
	}
	if _, err := root.Lstat(copyMarkerName); err == nil {
		seen.HasMarker = true
		seen.Nonce = nonceFrom(root, copyMarkerName)
	}
	if _, err := root.Lstat(copyReadmeName); err == nil {
		seen.HasReadme = true
		if seen.Nonce == "" {
			seen.Nonce = nonceFrom(root, copyReadmeName)
		}
	}
	return seen, nil
}

// nonceFrom reads the run id from the first line of a marker or README ("Run: <id>").
// It is read only to match, never to admit (design §4.9).
func nonceFrom(root *os.Root, name string) string {
	f, err := root.Open(name)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 256)
	n, _ := f.Read(buf)
	line, _, _ := strings.Cut(string(buf[:n]), "\n")
	id, ok := strings.CutPrefix(strings.TrimSpace(line), copyRunLinePrefix)
	if !ok {
		return ""
	}
	return strings.TrimSpace(id)
}

// copyRunLinePrefix starts the first line of the marker and of README.txt, which
// repeat the run id: the content nonce of §4.9.
const copyRunLinePrefix = "Run: "
