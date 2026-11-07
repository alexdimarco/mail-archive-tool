//go:build darwin

package tbrestore

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// mkdirPrivate and createPrivate (macOS; settled 2026-11-07 [R7-01]): an entry is
// readable by this account alone from the instant it exists. A folder made with mkdir
// takes Local Folders' inheritable access-list entries at birth, whatever its mode says,
// and a handle another account opens before FS.Private removes them keeps its right to
// list it. So an entry made straight in Local Folders (the container, the parent file)
// is made with mkdir_extended / open_extended and an empty access list flagged
// KAUTH_ACL_NO_INHERIT, which the kernel reads as "inherit nothing" (kauth_acl_inherit:
// "initial object creation via open_extended() and mkdir_extended() [may] reject
// inheritance"), at mode 0700 / 0600, which the umask can only narrow. Those calls take a
// path, not a folder handle, so the new entry is then bound to the root: the root's look
// at the name and the path's must be the same entry, of the kind made. Everything else is
// made inside the container, which has no inheritable entry, so mkdirat 0700 and openat
// 0600 through the root are owner-only at birth too. FS.Private then reads each back.
func mkdirPrivate(root *os.Root, name string) error {
	if strings.Contains(name, "/") {
		return root.Mkdir(name, 0o700)
	}
	path := filepath.Join(root.Name(), name)
	sec := noInheritFilesec()
	p, err := unix.BytePtrFromString(path)
	if err != nil {
		return err
	}
	if _, _, e := unix.Syscall6(unix.SYS_MKDIR_EXTENDED, uintptr(unsafe.Pointer(p)), kauthNone, kauthNone, 0o700, uintptr(unsafe.Pointer(&sec[0])), 0); e != 0 {
		return &os.PathError{Op: "mkdir", Path: name, Err: e}
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return bindCreated(root, name, fi, true)
}

func createPrivate(root *os.Root, name string) (wfile, error) {
	if strings.Contains(name, "/") {
		return root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	}
	path := filepath.Join(root.Name(), name)
	sec := noInheritFilesec()
	p, err := unix.BytePtrFromString(path)
	if err != nil {
		return nil, err
	}
	flags := unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_NOFOLLOW | unix.O_CLOEXEC
	fd, _, e := unix.Syscall6(unix.SYS_OPEN_EXTENDED, uintptr(unsafe.Pointer(p)), uintptr(flags), kauthNone, kauthNone, 0o600, uintptr(unsafe.Pointer(&sec[0])))
	if e != 0 {
		return nil, &os.PathError{Op: "open", Path: name, Err: e}
	}
	f := os.NewFile(fd, path)
	fi, err := f.Stat()
	if err == nil {
		err = bindCreated(root, name, fi, false)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// kauthNone is KAUTH_UID_NONE and KAUTH_GID_NONE (~0 - 100): owner and group left as made.
const kauthNone = uintptr(0xFFFFFF9B)

// noInheritFilesec is a kauth_filesec (native byte order, as the *_extended calls read
// it): magic, no owner or group GUID, an access list of no entries whose flags are
// KAUTH_ACL_NO_INHERIT.
func noInheritFilesec() []byte {
	const (
		filesecMagic = 0x012CC16D
		aclNoInherit = 1 << 17
	)
	b := make([]byte, 4+16+16+4+4)
	binary.NativeEndian.PutUint32(b[0:4], filesecMagic)
	binary.NativeEndian.PutUint32(b[36:40], 0)
	binary.NativeEndian.PutUint32(b[40:44], aclNoInherit)
	return b
}

// bindCreated checks that what the path made is what the root sees at name: the same
// entry, of the kind made, not a link (a folder on the way swapped between the two would
// show a different entry, and the write stops before anything is put in it).
func bindCreated(root *os.Root, name string, made os.FileInfo, dir bool) error {
	got, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !os.SameFile(made, got) || got.IsDir() != dir || got.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s: %w", name, errChanged)
	}
	return nil
}
