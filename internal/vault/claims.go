package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"

	"mail-archive-tool/internal/util"
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

// ManifestIsPlaintext reports whether path is a plaintext MailArchive manifest: a
// regular file whose first key is "version" with a plaintext format number, read
// from at most its first 4 KiB, never through a link or a pipe. An encrypted
// archive's envelope, a damaged file and anything else is not (design phase3 §4.5:
// plaintext files at a recorded path read as tampering, not as an emptied folder).
func ManifestIsPlaintext(path string) bool {
	if fi, err := os.Lstat(path); err != nil || !fi.Mode().IsRegular() {
		return false
	}
	f, err := util.OpenRegularFile(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4<<10)
	n, _ := io.ReadFull(f, head)
	dec := json.NewDecoder(bytes.NewReader(head[:n]))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return false
	}
	if key, err := dec.Token(); err != nil || key != "version" {
		return false
	}
	var version int
	if dec.Decode(&version) != nil {
		return false
	}
	return version >= 1 && version < EncryptedManifestVersion
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
