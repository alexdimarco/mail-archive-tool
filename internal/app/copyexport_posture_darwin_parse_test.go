//go:build darwin

package app

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// kauthEntry builds one 24-byte ACE of a kauth_filesec: a principal GUID, flags and rights.
func kauthEntry(guid []byte, flags, rights uint32) []byte {
	b := make([]byte, 24)
	copy(b, guid)
	binary.LittleEndian.PutUint32(b[16:], flags)
	binary.LittleEndian.PutUint32(b[20:], rights)
	return b
}

// kauthFilesec builds a kauth_filesec: magic, owner and group GUIDs, the entry count, flags,
// then the entries.
func kauthFilesec(count uint32, entries ...[]byte) []byte {
	b := make([]byte, 44)
	binary.LittleEndian.PutUint32(b[0:], kauthFilesecMagic)
	binary.LittleEndian.PutUint32(b[36:], count)
	for _, e := range entries {
		b = append(b, e...)
	}
	return b
}

func macUserGUID(uid uint32) []byte {
	g := []byte{0xFF, 0xFF, 0xEE, 0xEE, 0xDD, 0xDD, 0xCC, 0xCC, 0xBB, 0xBB, 0xAA, 0xAA, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(g[12:], uid)
	return g
}

func macGroupGUID(gid uint32) []byte {
	g := []byte{0xAB, 0xCD, 0xEF, 0xAB, 0xCD, 0xEF, 0xAB, 0xCD, 0xEF, 0xAB, 0xCD, 0xEF, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(g[12:], gid)
	return g
}

// covers: MA-532, R29, R12, S52
// Known item K2 of the extraction workspace's adversarial pass: the macOS posture read goes
// through getattrlist by a raw x/sys Syscall6 (no cgo, no `ls -le`), and its decoding of the
// kauth_filesec it returns was exercised by one real-ACL case only. This decodes synthetic
// filesecs through parseFilesec and macWho: an everyone-allow-read entry with file and
// directory inheritance, a deny entry with limit-inherit, an allow of a right that is not
// read, an entry of a kind that is neither (skipped), an unknown principal, the "no ACL"
// count and the refusals of a bad magic and of a count the buffer cannot hold; and the
// syscall itself on a path with no ACL (no entries, ok) and on a path that is not there (not
// ok).
func TestCopyExportMacFilesecIsDecoded(t *testing.T) {
	everyone := macGroupGUID(12)
	body := kauthFilesec(5,
		kauthEntry(everyone, kauthAcePermit|kauthAceFileInh|kauthAceDirInh, kauthVnodeReadData),
		kauthEntry(macUserGUID(501), kauthAceDeny|kauthAceLimitInh, kauthVnodeReadData),
		kauthEntry(macGroupGUID(20), kauthAcePermit|kauthAceOnlyInh, 0x4), // write only: not a read
		kauthEntry(macGroupGUID(80), 3, kauthVnodeReadData),               // neither permit nor deny
		kauthEntry([]byte("0123456789abcdef"), kauthAcePermit, kauthVnodeReadData),
	)
	got, ok := parseFilesec(body)
	if !ok || len(got) != 4 {
		t.Fatalf("parseFilesec: %d entries, ok %v; want 4 (the odd kind skipped)", len(got), ok)
	}
	want := []ceMacACE{
		{Allow: true, Who: "everyone", Read: true, FileInherit: true, DirInherit: true},
		{Allow: false, Who: "user 501", Read: true, Limit: true},
		{Allow: true, Who: "group staff", Read: false, Only: true},
		{Allow: true, Who: "another account", Read: true},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if es, ok := parseFilesec(kauthFilesec(kauthFilesecNoACL)); !ok || len(es) != 0 {
		t.Errorf("a filesec with no ACL: %v, %v; want none, ok", es, ok)
	}
	bad := kauthFilesec(1)
	binary.LittleEndian.PutUint32(bad[0:], 0xdeadbeef)
	if _, ok := parseFilesec(bad); ok {
		t.Error("a filesec with the wrong magic was read")
	}
	if _, ok := parseFilesec(kauthFilesec(1000, kauthEntry(everyone, kauthAcePermit, kauthVnodeReadData))); ok {
		t.Error("a filesec whose entry count passes its buffer was read")
	}

	// The syscall: a plain directory has no ACL entries, a missing path cannot be read.
	dir := t.TempDir()
	if es, ok := readMacACL(dir); !ok || len(es) != 0 {
		t.Errorf("readMacACL of a directory with no ACL: %v, %v; want none, ok", es, ok)
	}
	if _, ok := readMacACL(filepath.Join(dir, "missing")); ok {
		t.Error("readMacACL of a missing path said ok")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
}
