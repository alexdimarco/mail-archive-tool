//go:build windows

package tbrestore

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// syncDirFile: a folder cannot be flushed on Windows (copyexport_fs.go states it); its
// entries are on disk when the files in it are.
func syncDirFile(*os.File) error { return nil }

// isSharingViolation: antivirus or an indexer holding the file (ERROR_SHARING_VIOLATION,
// ERROR_LOCK_VIOLATION).
func isSharingViolation(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// removeSeenAt is RemoveSeen on Windows: each folder on the way, and then the entry
// itself, opened relative to the handle before it (NtCreateFile with RootDirectory),
// with FILE_OPEN_REPARSE_POINT so a link, junction or other reparse point is opened as
// itself and refused, never followed; each handle's identity and kind compared with the
// scan's; then the entry, opened with DELETE access, removed by its handle (its
// disposition set), so the object removed is exactly the one checked, wherever it has
// been moved meanwhile (design §9 ADV-02 note).
func removeSeenAt(top *os.File, parts []string, want []fs.FileInfo) error {
	cur := windows.Handle(top.Fd())
	var opened []*os.File
	defer func() {
		for _, f := range opened {
			f.Close()
		}
	}()
	name := strings.Join(parts, "/")
	for i, p := range parts {
		final := i == len(parts)-1
		access := uint32(windows.FILE_READ_ATTRIBUTES | windows.SYNCHRONIZE)
		opts := uint32(windows.FILE_SYNCHRONOUS_IO_NONALERT | windows.FILE_OPEN_REPARSE_POINT | windows.FILE_OPEN_FOR_BACKUP_INTENT)
		if final {
			access |= windows.DELETE
		} else {
			access |= windows.FILE_TRAVERSE | windows.FILE_LIST_DIRECTORY
			opts |= windows.FILE_DIRECTORY_FILE
		}
		h, err := ntOpenAt(cur, p, access, opts)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || (final && isSharingViolation(err)) {
				return &os.PathError{Op: "open", Path: strings.Join(parts[:i+1], "/"), Err: err}
			}
			if final {
				return &os.PathError{Op: "open", Path: name, Err: err}
			}
			return errChanged // not a folder now
		}
		f := os.NewFile(uintptr(h), p)
		opened = append(opened, f)
		fi, err := f.Stat()
		if err != nil || !sameEntry(want[i], fi) {
			return errChanged
		}
		cur = h
	}
	beforeUnlinkSeam(name)
	if err := deleteByHandle(cur); err != nil {
		return &os.PathError{Op: "delete", Path: name, Err: err}
	}
	return nil
}

// deleteByHandle removes the object the handle is open on, as os.Remove does on Windows
// (internal/syscall/windows Deleteat): POSIX semantics first, so the name goes at once
// even while another handle (antivirus, an indexer) still has the file open; on a system
// or file system without them (FAT32, older Windows), the classic delete-on-close.
func deleteByHandle(h windows.Handle) error {
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS | windows.FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE)
	err := windows.SetFileInformationByHandle(h, windows.FileDispositionInfoEx, (*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)))
	if err == nil {
		return nil
	}
	if !errors.Is(err, windows.ERROR_INVALID_PARAMETER) && !errors.Is(err, windows.ERROR_NOT_SUPPORTED) && !errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
		return err
	}
	deleteFile := byte(1) // FILE_DISPOSITION_INFO's one BOOLEAN, DeleteFile
	return windows.SetFileInformationByHandle(h, windows.FileDispositionInfo, &deleteFile, 1)
}

// ntOpenAt opens name relative to the folder handle dir; an NT status is returned as its
// Win32 error, so errors.Is finds fs.ErrNotExist and the sharing violation.
func ntOpenAt(dir windows.Handle, name string, access, options uint32) (windows.Handle, error) {
	objName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return 0, err
	}
	oa := &windows.OBJECT_ATTRIBUTES{RootDirectory: dir, ObjectName: objName}
	oa.Length = uint32(unsafe.Sizeof(*oa))
	var h windows.Handle
	var iosb windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&h, access, oa, &iosb, nil, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN, options, 0, 0)
	if err != nil {
		var st windows.NTStatus
		if errors.As(err, &st) {
			return 0, st.Errno()
		}
		return 0, err
	}
	return h, nil
}
