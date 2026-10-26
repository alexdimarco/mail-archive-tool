//go:build windows

package winsec

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// ErrNotOwnersOnly refuses a secret file another account owns or can read.
var ErrNotOwnersOnly = errors.New("can be read by other accounts or is not yours")

// CheckSecretFile judges an open password or recovery-key file by its handle
// (design-encrypted-archives-phase3 §3.4): its owner must be this process's user,
// and no allow entry may let any account but the user, SYSTEM or Administrators
// read it (DACLReadableByOthers). The read is verified on a real Windows box (the
// Phase 3 Windows lab); the decision is unit-tested on any OS.
func CheckSecretFile(h windows.Handle) error {
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("%w: its access list cannot be read: %w", ErrNotOwnersOnly, err)
	}
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	user := tu.User.Sid.String()
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || owner.String() != user {
		return fmt.Errorf("%w: it is not owned by you", ErrNotOwnersOnly)
	}
	if DACLReadableByOthers(sd.String(), user) {
		return fmt.Errorf("%w: other accounts can read it", ErrNotOwnersOnly)
	}
	return nil
}
