//go:build windows

package tbrestore

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// mkdirPrivate and createPrivate (Windows; settled 2026-11-07 [R7-01]): an entry is
// readable by this account alone from the instant it exists. A folder made with
// CreateDirectory and no security descriptor takes Local Folders' inherited entries (a
// folder on a second drive lets every signed-in account read), and a handle another
// account opens before FS.Private replaces them keeps the rights it was opened with. So
// each entry is made by one NtCreateFile, relative to its parent folder opened through the
// root (no name, link or ".." reaches outside it), exclusively (FILE_CREATE), with its
// security descriptor in the create call itself: this user its owner and a protected
// access list with one entry, this user's full control (inherited by what is made in a
// folder). Nothing inherited from Local Folders applies to it. A volume with no access
// lists (FAT32, exFAT) ignores the descriptor; FS.Private's read-back then fails, which
// stops an encrypted archive's restore whose plan did not ask a second time (R6-01).
// (macOS needs a different call on such a volume; Windows does not: NtCreateFile there
// takes the descriptor and ignores it, R7-01 independent F1, by reasoning, not run.)
func mkdirPrivate(root *os.Root, name string) error {
	h, err := createAt(root, name, true)
	if err != nil {
		return err
	}
	return windows.CloseHandle(h)
}

func createPrivate(root *os.Root, name string) (wfile, error) {
	h, err := createAt(root, name, false)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), filepath.Join(root.Name(), filepath.FromSlash(name))), nil
}

func createAt(root *os.Root, name string, dir bool) (windows.Handle, error) {
	parent, leaf := path.Split(name)
	parent = strings.TrimSuffix(parent, "/")
	if parent == "" {
		parent = "."
	}
	// Settled 2026-11-07 [R7-01 independent F2]: the last part goes to NtCreateFile raw,
	// past os.Root's checks, and NT reads '\' (and '/') as a folder separator and ':' as a
	// file's stream: such a part, or one filepath.IsLocal refuses (a device name, a
	// volume), is refused before anything is made, so FS's promise holds for it too.
	if leaf == "" || leaf == "." || leaf == ".." || strings.ContainsAny(leaf, `\/:`+"\x00") || !filepath.IsLocal(leaf) {
		return 0, &os.PathError{Op: "create", Path: name, Err: windows.ERROR_INVALID_NAME}
	}
	pf, err := root.Open(parent)
	if err != nil {
		return 0, err
	}
	defer pf.Close()
	access := uint32(windows.SYNCHRONIZE | windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL)
	opts := uint32(windows.FILE_SYNCHRONOUS_IO_NONALERT | windows.FILE_OPEN_REPARSE_POINT)
	if dir {
		access |= windows.FILE_LIST_DIRECTORY | windows.FILE_TRAVERSE
		opts |= windows.FILE_DIRECTORY_FILE
	} else {
		access |= windows.GENERIC_WRITE
		opts |= windows.FILE_NON_DIRECTORY_FILE
	}
	h, err := ntCreateWith(windows.Handle(pf.Fd()), leaf, access, opts, dir, true)
	if errors.Is(err, windows.ERROR_INVALID_OWNER) {
		// An owner this token may not assign (never this user, but be safe): the access list
		// alone, which is what keeps others out.
		h, err = ntCreateWith(windows.Handle(pf.Fd()), leaf, access, opts, dir, false)
	}
	if err != nil {
		return 0, &os.PathError{Op: "create", Path: name, Err: err}
	}
	return h, nil
}

// ownerOnlySD is the descriptor a new entry is made with: this user's alone, protected
// from inheritance; a folder's entry is inherited by what is made in it (OICI).
func ownerOnlySD(dir, withOwner bool) (*windows.SECURITY_DESCRIPTOR, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sid := tu.User.Sid.String()
	inherit := ""
	if dir {
		inherit = "OICI"
	}
	s := "D:P(A;" + inherit + ";FA;;;" + sid + ")"
	if withOwner {
		s = "O:" + sid + s
	}
	return windows.SecurityDescriptorFromString(s)
}

func ntCreateWith(parent windows.Handle, leaf string, access, opts uint32, dir, withOwner bool) (windows.Handle, error) {
	sd, err := ownerOnlySD(dir, withOwner)
	if err != nil {
		return 0, err
	}
	objName, err := windows.NewNTUnicodeString(leaf)
	if err != nil {
		return 0, err
	}
	oa := &windows.OBJECT_ATTRIBUTES{RootDirectory: parent, ObjectName: objName, SecurityDescriptor: sd}
	oa.Length = uint32(unsafe.Sizeof(*oa))
	attrs := uint32(windows.FILE_ATTRIBUTE_NORMAL)
	if dir {
		attrs = windows.FILE_ATTRIBUTE_DIRECTORY
	}
	var h windows.Handle
	var iosb windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&h, access, oa, &iosb, nil, attrs,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_CREATE, opts, 0, 0)
	if err != nil {
		var st windows.NTStatus
		if errors.As(err, &st) {
			return 0, st.Errno()
		}
		return 0, err
	}
	return h, nil
}
