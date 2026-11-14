package state

// The archive's own id (the B3 adversarial pass's A1; docs/design-gmail-dedup.md §13 "[B3
// adversarial fixes]"). The manifest has no id of its own, and a field there would be dropped by
// an older binary's save or make it a newer version. The plaintext archive's last-run record
// carries one instead: a backup run gives it the first time (ArchiveIDOrNew), and WriteLastRun
// carries it into every later record of the archive. A record an older binary writes drops it;
// the next run then gives a new id, so whatever this computer kept for the old id is asked
// again, never applied. A copy of the archive (a restored older copy, a cloned folder) carries
// the same id: it is the same archive.

import (
	"crypto/rand"
	"encoding/hex"
)

// archiveIDLen is the id's length: 16 random bytes in lowercase hex.
const archiveIDLen = 32

// ValidArchiveID says s is an id NewArchiveID could have made.
func ValidArchiveID(s string) bool {
	if len(s) != archiveIDLen {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// NewArchiveID is a new random archive id.
func NewArchiveID() string {
	var b [archiveIDLen / 2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "" // no randomness: no id, so nothing kept can ever be applied
	}
	return hex.EncodeToString(b[:])
}

// ArchiveID is the id the plaintext archive at out carries in its last-run record; "" when it
// has none, the record is absent, damaged, or an encrypted archive's.
func ArchiveID(out string) string {
	lr, st, _ := ReadLastRun(out)
	if st != LastRunPresent {
		return ""
	}
	return lr.Archive
}

// ArchiveIDOrNew is the archive's id, or a new one when it has none: a backup run's record.
func ArchiveIDOrNew(out string) string {
	if id := ArchiveID(out); id != "" {
		return id
	}
	return NewArchiveID()
}
