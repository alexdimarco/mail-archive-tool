//go:build windows

package winsec

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

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
	tok := windows.GetCurrentProcessToken()
	tu, err := tok.GetTokenUser()
	if err != nil {
		return err
	}
	def, _ := tokenDefaultOwner(tok)
	return checkSecretSD(sd, tu.User.Sid.String(), def)
}

// checkSecretSD is CheckSecretFile's decision on a security descriptor read, for the
// user whose SID is user. The owner may be the user, or the owner this process's
// token gives every file it makes (def): an administrator running elevated makes
// files Administrators own, its own password and recovery-key files included — and
// MailArchive's own lock check accepts the same owner (internal/instance). Who else
// may read the file is judged the same either way (P3-8's CI run on windows-latest).
func checkSecretSD(sd *windows.SECURITY_DESCRIPTOR, user, def string) error {
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || (owner.String() != user && (def == "" || owner.String() != def)) {
		return fmt.Errorf("%w: it is not owned by you", ErrNotOwnersOnly)
	}
	if DACLReadableByOthersFor(sd.String(), UserEntry(user)) {
		return fmt.Errorf("%w: other accounts can read it", ErrNotOwnersOnly)
	}
	return nil
}

// tokenOwner is the TOKEN_OWNER structure GetTokenInformation(TokenOwner) fills.
type tokenOwner struct{ Owner *windows.SID }

// tokenDefaultOwner is the SID new objects this token creates are owned by (as
// internal/instance reads it for the lock check).
func tokenDefaultOwner(tok windows.Token) (string, error) {
	var n uint32
	_ = windows.GetTokenInformation(tok, windows.TokenOwner, nil, 0, &n) // reports the size needed
	if n == 0 {
		return "", errors.New("no token owner size")
	}
	b := make([]byte, n)
	if err := windows.GetTokenInformation(tok, windows.TokenOwner, &b[0], n, &n); err != nil {
		return "", err
	}
	o := (*tokenOwner)(unsafe.Pointer(&b[0])).Owner
	if o == nil {
		return "", errors.New("no token owner")
	}
	return o.String(), nil // read before b can be collected
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
