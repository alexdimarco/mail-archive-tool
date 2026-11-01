//go:build windows

package archdelete

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/util"
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

// isNotEmpty reports a folder that could not be removed because it holds files.
func isNotEmpty(err error) bool { return errors.Is(err, errDirNotEmpty) }

// isAccessDenied reports the error Windows gives for deleting a read-only file.
func isAccessDenied(err error) bool { return errors.Is(err, syscall.ERROR_ACCESS_DENIED) }

// readOnlyBlocked keeps §9's read-only rule ahead of Go's own removal: on Windows
// os.Root.Remove deletes a read-only file itself, and where the file system or
// Windows build lacks FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE it clears the
// read-only mark first (internal/syscall/windows deleteatFallback) with no check of
// the file's links. So a read-only file is handed to it only when the delete may
// change it (mayClear: a recorded name, a ticked listed copy) and it has a single
// link; otherwise it is left, as access denied. A file that is not read-only, or
// cannot be read, is no matter here.
func readOnlyBlocked(path string, mayClear bool) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_READONLY == 0 {
		return nil
	}
	if !mayClear || info.NumberOfLinks != 1 {
		return &fs.PathError{Op: "remove", Path: path, Err: syscall.ERROR_ACCESS_DENIED}
	}
	return nil
}

// fileBasicInfo is FILE_BASIC_INFO: zero times are left as they are.
type fileBasicInfo struct {
	CreationTime, LastAccessTime, LastWriteTime, ChangeTime int64
	FileAttributes                                          uint32
	_                                                       uint32
}

// clearReadOnly clears the read-only attribute of the file at path (§9 [C11, O1]):
// through a handle opened without following a link at its name
// (FILE_FLAG_OPEN_REPARSE_POINT), only when the handle's file has a single link —
// changing a hard-linked file would change it everywhere it is linked — and never
// for a link. Its caller asks only for a name the archive recorded or a §4.3 copy
// the person ticked and the plan lists.
func clearReadOnly(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES|windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return fmt.Errorf("%s is a folder", path)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		if link, lerr := util.IsLinkLike(path); lerr != nil || link {
			return fmt.Errorf("%s is a link", path)
		}
	}
	if info.NumberOfLinks != 1 {
		return fmt.Errorf("%s has %d links: its read-only mark is not changed", path, info.NumberOfLinks)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_READONLY == 0 {
		return fmt.Errorf("%s is not read-only", path)
	}
	bi := fileBasicInfo{FileAttributes: info.FileAttributes &^ windows.FILE_ATTRIBUTE_READONLY}
	if bi.FileAttributes == 0 {
		bi.FileAttributes = windows.FILE_ATTRIBUTE_NORMAL
	}
	return windows.SetFileInformationByHandle(h, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&bi)), uint32(unsafe.Sizeof(bi)))
}
