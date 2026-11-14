//go:build windows

package util

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// createSecretTemp is os.CreateTemp with the file created under secretFileLabel: CreateFile
// is given the security descriptor, so the label is the file's from its first moment (the
// menu-bar S1 adversarial re-check r3, C1). A label the system will not set (a process
// below Medium integrity, a file system without labels) is an error: the secret is not
// written rather than written readable from below.
func createSecretTemp(dir, pattern string) (*os.File, error) {
	sd, err := windows.SecurityDescriptorFromString(secretFileLabel)
	if err != nil {
		return nil, err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	prefix, suffix := pattern, ""
	if i := strings.LastIndex(pattern, "*"); i >= 0 {
		prefix, suffix = pattern[:i], pattern[i+1:]
	}
	if strings.ContainsAny(prefix+suffix, `\/`) {
		return nil, errors.New("pattern contains a path separator")
	}
	for try := 0; try < 100; try++ {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, err
		}
		name := filepath.Join(dir, prefix+hex.EncodeToString(b[:])+suffix)
		p, err := windows.UTF16PtrFromString(name)
		if err != nil {
			return nil, err
		}
		h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
			sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if errors.Is(err, windows.ERROR_FILE_EXISTS) {
			continue
		}
		if err != nil {
			return nil, &os.PathError{Op: "createsecret", Path: name, Err: err}
		}
		return os.NewFile(uintptr(h), name), nil
	}
	return nil, &os.PathError{Op: "createsecret", Path: filepath.Join(dir, pattern), Err: os.ErrExist}
}
