//go:build darwin

package app

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"unsafe"

	"golang.org/x/sys/unix"
)

// macOS: the destination's inheritable ACL entries are read and named (design §4.7).
// Neither the standard library nor x/sys wraps acl_get_file or getattrlist, and cgo
// and shelling out to `ls -le` are barred (desktop P1), so the ACL comes from
// getattrlist(ATTR_CMN_EXTENDED_SECURITY), the call acl_get_file itself makes: a
// kauth_filesec the entries of which are decoded below.

const (
	attrBitMapCount         = 5
	attrCmnReturnedAttrs    = 0x80000000
	attrCmnExtendedSecurity = 0x00400000
	fsoptNoFollow           = 0x1

	kauthFilesecMagic = 0x012CC16D
	kauthFilesecNoACL = 0xffffffff

	kauthAceKindMask   = 0xf
	kauthAcePermit     = 1
	kauthAceDeny       = 2
	kauthAceFileInh    = 0x20
	kauthAceDirInh     = 0x40
	kauthAceLimitInh   = 0x80
	kauthAceOnlyInh    = 0x100
	kauthVnodeReadData = 0x2 // KAUTH_VNODE_READ_DATA, also LIST_DIRECTORY
)

type attrList struct {
	bitmapCount uint16
	reserved    uint16
	common      uint32
	vol         uint32
	dir         uint32
	file        uint32
	fork        uint32
}

// readMacACL returns path's ACL entries (none for a path with no ACL). ok is false
// when the ACL could not be read.
func readMacACL(path string) (entries []ceMacACE, ok bool) {
	p, err := unix.BytePtrFromString(path)
	if err != nil {
		return nil, false
	}
	al := attrList{bitmapCount: attrBitMapCount, common: attrCmnReturnedAttrs | attrCmnExtendedSecurity}
	buf := make([]byte, 64<<10)
	_, _, errno := unix.Syscall6(unix.SYS_GETATTRLIST, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&al)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), fsoptNoFollow, 0)
	if errno != 0 {
		return nil, false
	}
	le := binary.LittleEndian
	if len(buf) < 32 || le.Uint32(buf[0:4]) < 32 {
		return nil, true // nothing past the returned-attributes set: no ACL
	}
	// buf: length u32, attribute_set_t (five u32), then the attrreference_t of the
	// extended security (offset int32 from its own start, length u32).
	if le.Uint32(buf[4:8])&attrCmnExtendedSecurity == 0 {
		return nil, true
	}
	refOff := 24
	off := int(int32(le.Uint32(buf[refOff : refOff+4])))
	n := int(le.Uint32(buf[refOff+4 : refOff+8]))
	start := refOff + off
	if off <= 0 || n < 44 || start < 0 || start+n > len(buf) {
		return nil, true
	}
	return parseFilesec(buf[start : start+n])
}

// parseFilesec decodes a kauth_filesec: magic, owner and group GUIDs, then the ACL
// (entry count, flags, 24-byte entries of a GUID, flags and rights).
func parseFilesec(b []byte) ([]ceMacACE, bool) {
	le := binary.LittleEndian
	if len(b) < 44 || le.Uint32(b[0:4]) != kauthFilesecMagic {
		return nil, false
	}
	count := le.Uint32(b[36:40])
	if count == kauthFilesecNoACL {
		return nil, true
	}
	body := b[44:]
	if int(count) > len(body)/24 {
		return nil, false
	}
	var out []ceMacACE
	for i := 0; i < int(count); i++ {
		e := body[i*24 : i*24+24]
		flags := le.Uint32(e[16:20])
		rights := le.Uint32(e[20:24])
		kind := flags & kauthAceKindMask
		if kind != kauthAcePermit && kind != kauthAceDeny {
			continue
		}
		out = append(out, ceMacACE{
			Allow: kind == kauthAcePermit, Who: macWho(e[0:16]), Read: rights&kauthVnodeReadData != 0,
			FileInherit: flags&kauthAceFileInh != 0, DirInherit: flags&kauthAceDirInh != 0,
			Limit: flags&kauthAceLimitInh != 0, Only: flags&kauthAceOnlyInh != 0,
		})
	}
	return out, true
}

// macWho names an ACE's principal GUID. The compatibility GUIDs encode a user or
// group id in their last four bytes (big-endian) and "everyone" is 0xC; any other GUID
// belongs to a directory account the line cannot look up without libc's
// membership service, so it is named as another account.
func macWho(g []byte) string {
	user := []byte{0xFF, 0xFF, 0xEE, 0xEE, 0xDD, 0xDD, 0xCC, 0xCC, 0xBB, 0xBB, 0xAA, 0xAA}
	group := []byte{0xAB, 0xCD, 0xEF, 0xAB, 0xCD, 0xEF, 0xAB, 0xCD, 0xEF, 0xAB, 0xCD, 0xEF}
	id := binary.BigEndian.Uint32(g[12:16])
	switch {
	case string(g[:12]) == string(user) && id == 0xC:
		return "everyone"
	case string(g[:12]) == string(user):
		return "user " + strconv.FormatUint(uint64(id), 10)
	case string(g[:12]) == string(group):
		return "group " + strconv.FormatUint(uint64(id), 10)
	}
	return "another account"
}

func meMac() string {
	if u, err := user.Current(); err == nil {
		if uid, err := strconv.ParseUint(u.Uid, 10, 32); err == nil {
			return fmt.Sprintf("user %d", uid)
		}
	}
	return ""
}

func osInheritedPosture(dir string) CopyPosture {
	acl, ok := readMacACL(dir)
	return macPosture(acl, meMac(), ok)
}

func osRealizedPosture(path string, isDir bool) CopyPosture {
	acl, ok := readMacACL(path)
	p := macRealized(acl, meMac(), ok)
	if p.Kind != postureKindPrivate {
		return p
	}
	if fi, err := os.Lstat(path); err == nil && fi.Mode().Perm()&0o077 != 0 {
		return newPosture(postureKindReaders, []string{"other accounts on this computer"})
	}
	return p
}
