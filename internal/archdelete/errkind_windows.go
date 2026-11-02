//go:build windows

package archdelete

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows errors §8 (a) tells apart.
const (
	errSharingViolation   = syscall.Errno(32)
	errLockViolation      = syscall.Errno(33)
	errUserMappedFile     = syscall.Errno(1224)
	errNotReady           = syscall.Errno(21)
	errDevNotExist        = syscall.Errno(55)
	errDeviceNotConnected = syscall.Errno(1167)
	errDirNotEmpty        = syscall.Errno(145)
)

// causeOf is the kind of error that kept a file, for its remedy (§8 (a)).
func causeOf(err error) Cause {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return CauseOther
	}
	switch errno {
	case errSharingViolation, errLockViolation, errUserMappedFile:
		return CauseInUse
	case syscall.ERROR_ACCESS_DENIED:
		return CauseDenied
	case errNotReady, errDevNotExist, errDeviceNotConnected:
		return CauseDeviceGone
	}
	return CauseOther
}

// heldIn reports a file rel (native separators) inside root that another program holds open so
// that it cannot be deleted now: a handle asking to delete it is refused as in use (a sharing
// violation, a lock, a mapped file). It opens and closes that handle only; nothing is changed.
func heldIn(root *os.Root, rel string) bool {
	h, err := openAt(root, rel, windows.DELETE)
	if err != nil {
		return causeOf(err) == CauseInUse
	}
	windows.CloseHandle(h)
	return false
}

// isNotEmpty reports a folder that could not be removed because it holds files.
func isNotEmpty(err error) bool { return errors.Is(err, errDirNotEmpty) }

// isAccessDenied reports the error Windows gives for deleting a read-only file.
func isAccessDenied(err error) bool { return errors.Is(err, syscall.ERROR_ACCESS_DENIED) }

// deniedRemedy is §8 (a)'s remedy for access denied.
const deniedRemedy = "Windows security or antivirus blocked it: allow MailArchive or delete these by hand."

// deniedNamesFolders is false here: Windows refuses a file for the file itself (security, an
// antivirus, a read-only file), not for its folder, so the remedy names no folder.
const deniedNamesFolders = false

// openAttrFn opens the file rel names inside root for its attributes (openAt); a
// test seam for a file whose attributes cannot be read.
var openAttrFn = openAt

// openAt opens the file rel (native separators) names inside root, with access,
// without following a link at its name: through a handle relative to its folder's
// handle, which root opens — the lookup os.Root.Remove makes, so the file checked is
// the file removed, with no MAX_PATH limit and no second lookup of the archive
// folder's path (del-s1b-A1-04, A2-07).
func openAt(root *os.Root, rel string, access uint32) (windows.Handle, error) {
	dir, base := filepath.Split(rel)
	d, err := root.Open(filepath.Clean(dir))
	if err != nil {
		return 0, err
	}
	defer d.Close()
	name, err := windows.NewNTUnicodeString(base)
	if err != nil {
		return 0, err
	}
	oa := windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(d.Fd()), ObjectName: name, Attributes: windows.OBJ_CASE_INSENSITIVE}
	oa.Length = uint32(unsafe.Sizeof(oa))
	var h windows.Handle
	var iosb windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&h, access|windows.SYNCHRONIZE, &oa, &iosb, nil, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN,
		windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_FOR_BACKUP_INTENT, 0, 0)
	if err != nil {
		var st windows.NTStatus
		if errors.As(err, &st) {
			return 0, st.Errno()
		}
		return 0, err
	}
	return h, nil
}

// readOnlyBlocked keeps §9's read-only rule ahead of Go's own removal: on Windows
// os.Root.Remove deletes a read-only file itself, and where the file system or
// Windows build lacks FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE it clears the
// read-only mark first (internal/syscall/windows deleteatFallback) with no check of
// the file's links. So a read-only file is handed to it only when the delete may
// change it (mayClear: a recorded name, a ticked listed copy) and it has a single
// link; otherwise it is left, as access denied. A file that is not read-only is no
// matter here, and one already gone is the removal's to report; a file whose
// attributes cannot be read is left with that error — never handed to Go's removal
// unchecked (del-s1b-A1-04, A2-07).
func readOnlyBlocked(root *os.Root, rel string, mayClear bool) error {
	h, err := openAttrFn(root, rel, windows.FILE_READ_ATTRIBUTES)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return &fs.PathError{Op: "remove", Path: rel, Err: err}
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return &fs.PathError{Op: "remove", Path: rel, Err: err}
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_READONLY == 0 {
		return nil
	}
	if !mayClear || info.NumberOfLinks != 1 {
		return &fs.PathError{Op: "remove", Path: rel, Err: syscall.ERROR_ACCESS_DENIED}
	}
	return nil
}

// fileBasicInfo is FILE_BASIC_INFO: zero times are left as they are.
type fileBasicInfo struct {
	CreationTime, LastAccessTime, LastWriteTime, ChangeTime int64
	FileAttributes                                          uint32
	_                                                       uint32
}

// fileAttributeTagInfo is FILE_ATTRIBUTE_TAG_INFO.
type fileAttributeTagInfo struct {
	FileAttributes uint32
	ReparseTag     uint32
}

// reparseTagNameSurrogate is the bit a reparse tag carries when it names another
// place (a symbolic link, a junction, a mount point), as util.IsLinkLike reads it.
const reparseTagNameSurrogate = 0x20000000

// clearReadOnly clears the read-only attribute of the file rel names inside root (§9
// [C11, O1]): through a handle opened as readOnlyBlocked's is, without following a
// link at its name, only when the handle's file has a single link — changing a
// hard-linked file would change it everywhere it is linked — and never for a link.
// Its caller asks only for a name the archive recorded or a §4.3 copy the person
// ticked and the plan lists.
func clearReadOnly(root *os.Root, rel string) error {
	h, err := openAttrFn(root, rel, windows.FILE_READ_ATTRIBUTES|windows.FILE_WRITE_ATTRIBUTES)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return fmt.Errorf("%s is a folder", rel)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		var tag fileAttributeTagInfo
		if err := windows.GetFileInformationByHandleEx(h, windows.FileAttributeTagInfo, (*byte)(unsafe.Pointer(&tag)), uint32(unsafe.Sizeof(tag))); err != nil || tag.ReparseTag&reparseTagNameSurrogate != 0 {
			return fmt.Errorf("%s is a link", rel)
		}
	}
	if info.NumberOfLinks != 1 {
		return fmt.Errorf("%s has %d links: its read-only mark is not changed", rel, info.NumberOfLinks)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_READONLY == 0 {
		return fmt.Errorf("%s is not read-only", rel)
	}
	bi := fileBasicInfo{FileAttributes: info.FileAttributes &^ windows.FILE_ATTRIBUTE_READONLY}
	if bi.FileAttributes == 0 {
		bi.FileAttributes = windows.FILE_ATTRIBUTE_NORMAL
	}
	return windows.SetFileInformationByHandle(h, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&bi)), uint32(unsafe.Sizeof(bi)))
}
