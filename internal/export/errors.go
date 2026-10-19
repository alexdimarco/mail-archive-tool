package export

import (
	"errors"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// FolderError is the exporter's failure to create a folder's output directory
// (MkdirAll): ENAMETOOLONG for a deep or long CJK name, a regular file where the
// folder should be, an ACL (N1). It costs that folder only, unless the cause is a
// run-fatal one (IsRunFatal). Its text is what Export has always returned.
type FolderError struct {
	Dir string // the directory that could not be created
	Err error
}

func (e *FolderError) Error() string { return "create output dir " + e.Dir + ": " + e.Err.Error() }
func (e *FolderError) Unwrap() error { return e.Err }

// ItemError is the exporter's failure to render or write one message's files: it
// costs that message only, unless the cause is run-fatal. Path is the file being
// written ("" for a render). Its text is what Export has always returned.
type ItemError struct {
	Path string
	msg  string // "write <path>" or "render <subject>", before the cause
	Err  error
}

func (e *ItemError) Error() string { return e.msg + ": " + e.Err.Error() }
func (e *ItemError) Unwrap() error { return e.Err }

// ErrUnsafeToken matches (errors.Is) the exporter's refusal to write under a
// store token that is not a single safe path segment (a tampered manifest, INS2-1):
// run-fatal, because the next message of that store would be refused the same way.
var ErrUnsafeToken = errors.New("unsafe store token")

// unsafeTokenError is ErrUnsafeToken with the text Export has always returned.
type unsafeTokenError struct{ msg string }

func (e *unsafeTokenError) Error() string        { return e.msg }
func (e *unsafeTokenError) Is(target error) bool { return target == ErrUnsafeToken }

// IsRunFatal reports whether err stops the whole run instead of costing one folder
// or one item (fault-isolation §4.6, FC12): the archive's volume is full (ENOSPC,
// EDQUOT), read-only (EROFS), gone or failing (EIO, ENXIO, ENODEV, ENOTCONN, ESTALE,
// EHOSTDOWN), their Windows codes (windowsRunFatal), or the store token is unsafe.
// Anything else — permissions, a name too long, a file in a folder's place — is the
// folder's or the item's own.
func IsRunFatal(err error) bool { return RunFatalAt(err) }

// RunFatalAt is IsRunFatal that also counts Windows' "path not found" (3) as fatal
// when the error names one of roots (the archive root or a store's directory): that
// code is an ordinary missing parent anywhere else.
func RunFatalAt(err error, roots ...string) bool { return FatalKindAt(err, roots...) != FatalNone }

// FatalKind is why an error is run-fatal, so the run words each cause with its own
// remedy (§7.5).
type FatalKind int

const (
	FatalNone     FatalKind = iota // not run-fatal: one folder's or one item's own
	FatalFull                      // the drive is full or over its quota
	FatalReadOnly                  // the location is read-only or write-protected
	FatalGone                      // the volume or share is gone, not ready or failing
	FatalToken                     // the store token is unsafe (ErrUnsafeToken)
)

// FatalKindAt classifies err (see RunFatalAt).
func FatalKindAt(err error, roots ...string) FatalKind {
	if err == nil {
		return FatalNone
	}
	if errors.Is(err, ErrUnsafeToken) {
		return FatalToken
	}
	var en syscall.Errno
	if !errors.As(err, &en) {
		return FatalNone
	}
	if runtime.GOOS == "windows" {
		return windowsFatalKind(uintptr(en), errorNames(err, roots))
	}
	switch en {
	case syscall.ENOSPC, syscall.EDQUOT:
		return FatalFull
	case syscall.EROFS:
		return FatalReadOnly
	case syscall.EIO, syscall.ENXIO, syscall.ENODEV, syscall.ENOTCONN, syscall.ESTALE, syscall.EHOSTDOWN:
		return FatalGone
	}
	return FatalNone
}

// errorNames reports whether err is a path error naming one of roots.
func errorNames(err error, roots []string) bool {
	var pe *fs.PathError
	if !errors.As(err, &pe) {
		return false
	}
	p := filepath.Clean(pe.Path)
	for _, r := range roots {
		if r != "" && strings.EqualFold(p, filepath.Clean(r)) {
			return true
		}
	}
	return false
}

// windowsRunFatal classifies a Windows error number by the design's list (§4.6,
// FC12): 112 ERROR_DISK_FULL, 39 ERROR_HANDLE_DISK_FULL, 19 ERROR_WRITE_PROTECT, 21
// ERROR_NOT_READY, 53 ERROR_BAD_NETPATH, 55 ERROR_DEV_NOT_EXIST, 64
// ERROR_NETNAME_DELETED, 1167 ERROR_DEVICE_NOT_CONNECTED and 1295 are the volume's;
// 3 ERROR_PATH_NOT_FOUND is fatal only when onRoot (the archive root itself is
// gone), since it is an ordinary missing parent anywhere else.
func windowsRunFatal(code uintptr, onRoot bool) bool {
	return windowsFatalKind(code, onRoot) != FatalNone
}

// windowsFatalKind is windowsRunFatal with the reason: 112 and 39 are a full disk,
// 19 a write-protected one, the rest a volume that is gone.
func windowsFatalKind(code uintptr, onRoot bool) FatalKind {
	switch code {
	case 112, 39:
		return FatalFull
	case 19:
		return FatalReadOnly
	case 21, 53, 55, 64, 1167, 1295:
		return FatalGone
	case 3:
		if onRoot {
			return FatalGone
		}
	}
	return FatalNone
}
