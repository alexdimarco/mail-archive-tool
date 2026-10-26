//go:build linux

package keyring

import (
	"encoding/binary"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// extendedACL reports whether the open file f carries a POSIX access list with
// entries beyond its mode — a named user or group, or a mask — any of which may
// let another account read it. An access list that cannot be read is judged to
// have one, unless the file system has none at all.
func extendedACL(f *os.File) bool {
	buf := make([]byte, 4096)
	n, err := unix.Fgetxattr(int(f.Fd()), "system.posix_acl_access", buf)
	switch {
	case errors.Is(err, unix.ENODATA), errors.Is(err, unix.EOPNOTSUPP):
		return false
	case err != nil:
		return true
	}
	return aclHasExtraEntries(buf[:n])
}

// aclHasExtraEntries reads the xattr form of a POSIX access list (a 4-byte
// version, then 8-byte entries: tag, permissions, id) and reports an entry other
// than the owner's, the owning group's and other's.
func aclHasExtraEntries(b []byte) bool {
	const userObj, groupObj, other = 0x01, 0x04, 0x20
	if len(b) < 4 || (len(b)-4)%8 != 0 {
		return true
	}
	for i := 4; i+8 <= len(b); i += 8 {
		switch binary.LittleEndian.Uint16(b[i:]) {
		case userObj, groupObj, other:
		default:
			return true
		}
	}
	return false
}
