//go:build windows

package app

import (
	"errors"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows placement (design §4.9, RR2-C10): the handle-relative rename Go's
// os.Root.Rename makes (FILE_RENAME_INFORMATION_EX with RootDirectory set to the
// directory handle), without its replace-if-exists flag, so an existing name is a
// STATUS_OBJECT_NAME_COLLISION and never a replacement. Where a volume rejects the Ex
// information class (FAT and exFAT may), the plain FILE_RENAME_INFORMATION with
// ReplaceIfExists FALSE and the same RootDirectory. The path-based MoveFileEx is
// never used. This code compiles on every CI leg and runs on the Windows one.

const (
	fileRenameInformationEx = 65 // FileRenameInformationEx; x/sys defines only the plain class

	statusObjectNameCollision = 0xC0000035
	statusInvalidInfoClass    = 0xC0000003
	statusNotImplemented      = 0xC0000002
	statusInvalidParameter    = 0xC000000D
	statusNotSupported        = 0xC00000BB
)

// rawControl runs fn with f's handle held.
func rawControl(f *os.File, fn func(h windows.Handle)) error {
	sc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	return sc.Control(func(h uintptr) { fn(windows.Handle(h)) })
}

func existsErr(oldn, newn string) error {
	return &os.LinkError{Op: "rename", Old: oldn, New: newn, Err: syscall.ERROR_ALREADY_EXISTS}
}

func isReparsePoint(fi os.FileInfo) bool {
	if a, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		return a.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
	}
	return false
}

// renameInfo builds a FILE_RENAME_INFORMATION(_EX) for the single-component name,
// relative to dir.
func renameInfo(dir windows.Handle, name string, flags uint32) ([]byte, error) {
	u, err := windows.UTF16FromString(name)
	if err != nil {
		return nil, err
	}
	u = u[:len(u)-1] // no terminator: FileNameLength counts bytes
	ptr := int(unsafe.Sizeof(uintptr(0)))
	rootOff := (4 + ptr - 1) / ptr * ptr
	lenOff := rootOff + ptr
	nameOff := lenOff + 4
	buf := make([]byte, nameOff+2*len(u)+2)
	*(*uint32)(unsafe.Pointer(&buf[0])) = flags
	*(*uintptr)(unsafe.Pointer(&buf[rootOff])) = uintptr(dir)
	*(*uint32)(unsafe.Pointer(&buf[lenOff])) = uint32(2 * len(u))
	for i, c := range u {
		*(*uint16)(unsafe.Pointer(&buf[nameOff+2*i])) = c
	}
	return buf, nil
}

// renameNoReplace gives oldn the name newn inside d without replacing anything.
func renameNoReplace(d *os.Root, oldn, newn string) error {
	dirf, err := d.Open(".")
	if err != nil {
		return err
	}
	defer dirf.Close()
	var out error
	cerr := rawControl(dirf, func(dir windows.Handle) { out = ntRenameNoReplace(dir, oldn, newn) })
	if cerr != nil {
		return cerr
	}
	return out
}

func ntRenameNoReplace(dir windows.Handle, oldn, newn string) error {
	name, err := windows.NewNTUnicodeString(oldn)
	if err != nil {
		return err
	}
	oa := &windows.OBJECT_ATTRIBUTES{RootDirectory: dir, ObjectName: name, Attributes: windows.OBJ_CASE_INSENSITIVE}
	oa.Length = uint32(unsafe.Sizeof(*oa))
	var h windows.Handle
	var iosb windows.IO_STATUS_BLOCK
	// The source is opened relative to the directory handle, with DELETE access (a
	// rename needs it), and the share modes that let an antivirus scan hold it too.
	st := windows.NtCreateFile(&h, windows.DELETE|windows.SYNCHRONIZE|windows.FILE_READ_ATTRIBUTES, oa, &iosb, nil, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN,
		windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT, 0, 0)
	if st != nil {
		return ntErr("rename", oldn, newn, st)
	}
	defer windows.CloseHandle(h)

	info, err := renameInfo(dir, newn, 0) // Flags 0: no FILE_RENAME_REPLACE_IF_EXISTS, no POSIX semantics
	if err != nil {
		return err
	}
	st = windows.NtSetInformationFile(h, &iosb, &info[0], uint32(len(info)), fileRenameInformationEx)
	if code, ok := st.(windows.NTStatus); ok && (code == statusInvalidInfoClass || code == statusNotImplemented ||
		code == statusInvalidParameter || code == statusNotSupported) {
		// The volume rejects the Ex class: the plain one with ReplaceIfExists FALSE.
		info, err = renameInfo(dir, newn, 0)
		if err != nil {
			return err
		}
		st = windows.NtSetInformationFile(h, &iosb, &info[0], uint32(len(info)), windows.FileRenameInformation)
	}
	if st != nil {
		return ntErr("rename", oldn, newn, st)
	}
	return nil
}

// ntErr maps an NTSTATUS to the error os would return for it.
func ntErr(op, oldn, newn string, st error) error {
	code, ok := st.(windows.NTStatus)
	if !ok {
		return &os.LinkError{Op: op, Old: oldn, New: newn, Err: st}
	}
	if code == statusObjectNameCollision {
		return existsErr(oldn, newn)
	}
	return &os.LinkError{Op: op, Old: oldn, New: newn, Err: code.Errno()}
}

// readIdentity reads the volume serial number and file ID of an open directory, its
// volume's filesystem name and its creation time.
func readIdentity(f *os.File) (ceIdentity, error) {
	var id ceIdentity
	var ferr error
	if err := rawControl(f, func(h windows.Handle) {
		var info windows.ByHandleFileInformation
		if e := windows.GetFileInformationByHandle(h, &info); e != nil {
			ferr = e
			return
		}
		id.Vol = info.VolumeSerialNumber
		id.FileID = uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)
		id.Birth = info.CreationTime.Nanoseconds()
		fsName := make([]uint16, 64)
		if e := windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, nil, &fsName[0], uint32(len(fsName))); e == nil {
			id.FSType = windows.UTF16ToString(fsName)
		}
	}); err != nil {
		return id, err
	}
	if ferr != nil {
		return id, ferr
	}
	if !id.isWindowsID() {
		return id, errors.New("the volume reported no file identity")
	}
	return id, nil
}
