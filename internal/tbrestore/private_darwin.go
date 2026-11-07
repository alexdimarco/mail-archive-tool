//go:build darwin

package tbrestore

import (
	"encoding/binary"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// makePrivate (macOS): an access list inherited from Local Folders ("everyone may read",
// file_inherit) applies whatever the mode says, so the entry, opened through the root
// without following a link, gets its mode and its access list removed in one call
// (fchmod_extended with _FILESEC_REMOVE_ACL, what `chmod -N` does; cgo and shelling out
// are barred), then is read back: this user's, no group or other bit, and no access-list
// entry (fgetattrlist ATTR_CMN_EXTENDED_SECURITY, as the copy export reads one).
func makePrivate(root *os.Root, name string, dir bool) error {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	mode := uintptr(0o600)
	if dir {
		mode = 0o700
	}
	const (
		kauthNone       = uintptr(0xFFFFFF9B) // KAUTH_UID_NONE, KAUTH_GID_NONE: ~0 - 100
		filesecRemoveAC = 1                   // _FILESEC_REMOVE_ACL
	)
	if _, _, e := unix.Syscall6(unix.SYS_FCHMOD_EXTENDED, f.Fd(), kauthNone, kauthNone, mode, filesecRemoveAC, 0); e != 0 {
		return e
	}
	if err := ownerOnlyMode(f); err != nil {
		return err
	}
	n, ok := aclEntries(f)
	if !ok || n != 0 {
		return errNotPrivate
	}
	return nil
}

// aclEntries counts f's access-list entries; ok false when they cannot be read.
func aclEntries(f *os.File) (int, bool) {
	type attrList struct {
		bitmapCount                    uint16
		reserved                       uint16
		common, vol, dir, file, forkAt uint32
	}
	const (
		attrCmnReturnedAttrs    = 0x80000000
		attrCmnExtendedSecurity = 0x00400000
		filesecMagic            = 0x012CC16D
		filesecNoACL            = 0xffffffff
	)
	al := attrList{bitmapCount: 5, common: attrCmnReturnedAttrs | attrCmnExtendedSecurity}
	buf := make([]byte, 64<<10)
	if _, _, e := unix.Syscall6(unix.SYS_FGETATTRLIST, f.Fd(), uintptr(unsafe.Pointer(&al)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0); e != 0 {
		return 0, false
	}
	le := binary.LittleEndian
	if le.Uint32(buf[0:4]) < 32 || le.Uint32(buf[4:8])&attrCmnExtendedSecurity == 0 {
		return 0, true // no extended security returned: no access list
	}
	off := int(int32(le.Uint32(buf[24:28])))
	n := int(le.Uint32(buf[28:32]))
	start := 24 + off
	if off <= 0 || n < 44 || start+n > len(buf) {
		return 0, true
	}
	b := buf[start : start+n]
	if le.Uint32(b[0:4]) != filesecMagic {
		return 0, false
	}
	count := le.Uint32(b[36:40])
	if count == filesecNoACL {
		return 0, true
	}
	return int(count), true
}
