//go:build windows

package graphconfig

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DefaultSecretStore on Windows is the user's Credential Manager vault, so the
// client secret is not a plaintext file on disk (OR2). Entries are generic
// credentials under the current user's vault.
func DefaultSecretStore() (SecretStore, error) { return credmanStore{}, nil }

var (
	advapi32        = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	errorNotFound           = syscall.Errno(1168) // ERROR_NOT_FOUND
)

// credentialW mirrors the Win32 CREDENTIALW struct (wincred.h).
type credentialW struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type credmanStore struct{}

func (credmanStore) Name() string { return "Windows Credential Manager" }

func (credmanStore) target(account string) string { return "mailarchive:" + account }

func (s credmanStore) Set(account, secret string) error {
	target, err := windows.UTF16PtrFromString(s.target(account))
	if err != nil {
		return err
	}
	blob := []byte(secret)
	var blobPtr *byte
	if len(blob) > 0 {
		blobPtr = &blob[0]
	}
	cred := credentialW{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(blob)),
		CredentialBlob:     blobPtr,
		Persist:            credPersistLocalMachine,
	}
	r, _, callErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0)
	if r == 0 {
		return fmt.Errorf("CredWrite %s: %w", account, callErr)
	}
	return nil
}

func (s credmanStore) Get(account string) (string, error) {
	target, err := windows.UTF16PtrFromString(s.target(account))
	if err != nil {
		return "", err
	}
	var pcred *credentialW
	r, _, callErr := procCredReadW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&pcred)))
	if r == 0 {
		if en, ok := callErr.(syscall.Errno); ok && en == errorNotFound {
			return "", fmt.Errorf("no stored secret for %s: %w", account, os.ErrNotExist)
		}
		return "", fmt.Errorf("CredRead %s: %w", account, callErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(pcred)))
	if pcred.CredentialBlobSize == 0 || pcred.CredentialBlob == nil {
		return "", nil
	}
	buf := unsafe.Slice(pcred.CredentialBlob, pcred.CredentialBlobSize)
	return string(buf), nil
}

func (s credmanStore) Delete(account string) error {
	target, err := windows.UTF16PtrFromString(s.target(account))
	if err != nil {
		return err
	}
	r, _, callErr := procCredDeleteW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	if r == 0 {
		if en, ok := callErr.(syscall.Errno); ok && en == errorNotFound {
			return nil // already absent
		}
		return fmt.Errorf("CredDelete %s: %w", account, callErr)
	}
	return nil
}
