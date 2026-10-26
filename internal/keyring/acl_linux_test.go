//go:build linux

package keyring

import (
	"encoding/binary"
	"testing"

	"mail-archive-tool/internal/assure"
)

// acl builds the xattr form of a POSIX access list from entry tags.
func acl(tags ...uint16) []byte {
	b := []byte{2, 0, 0, 0}
	for _, tag := range tags {
		e := make([]byte, 8)
		binary.LittleEndian.PutUint16(e, tag)
		binary.LittleEndian.PutUint16(e[2:], 4)
		b = append(b, e...)
	}
	return b
}

// covers: MA-790, MA-782
// On Linux a secret file's POSIX access list is judged with its mode: entries for
// the owner, the owning group and other only are the mode; a named user, a named
// group or a mask may let another account read it, and a list that does not read
// is judged to (the P3-1a pass, SEC-06).
func TestAnAccessListBeyondTheModeIsSeen(t *testing.T) {
	if aclHasExtraEntries(acl(0x01, 0x04, 0x20)) {
		t.Fatal("the base entries alone were judged an extended access list")
	}
	for name, b := range map[string][]byte{
		"a named user":  acl(0x01, 0x02, 0x04, 0x10, 0x20),
		"a named group": acl(0x01, 0x04, 0x08, 0x10, 0x20),
		"torn":          {2, 0, 0},
	} {
		if !aclHasExtraEntries(b) {
			t.Errorf("%s was not seen", name)
		}
	}
	assure.Reached(t, true, "access lists judged")
}
