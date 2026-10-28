//go:build windows

package winsec

import (
	"errors"
	"fmt"
	"strings"

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
	return checkSecretSD(sd, tu.User.Sid.String())
}

// checkSecretSD is CheckSecretFile's decision on a security descriptor read, for the
// user whose SID is user.
func checkSecretSD(sd *windows.SECURITY_DESCRIPTOR, user string) error {
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || owner.String() != user {
		return fmt.Errorf("%w: it is not owned by you", ErrNotOwnersOnly)
	}
	if DACLReadableByOthersFor(sd.String(), UserEntry(user)) {
		return fmt.Errorf("%w: other accounts can read it", ErrNotOwnersOnly)
	}
	return nil
}

// UserEntry tells whether an access-list account field names the account whose SID
// is userSid: the SID as written, or an alias Windows writes for it — the built-in
// Administrator (RID 500) is written "LA" — resolved on this computer (the P3-8 fixes'
// pass, WINACL-1). A group's alias never resolves to a user's SID.
func UserEntry(userSid string) func(field string) bool {
	return func(field string) bool {
		switch {
		case userSid == "":
			return false
		case field == userSid:
			return true
		case strings.HasPrefix(field, "S-"):
			return false // a SID written out is compared as written
		}
		sid, err := windows.StringToSid(field)
		return err == nil && sid.String() == userSid
	}
}
