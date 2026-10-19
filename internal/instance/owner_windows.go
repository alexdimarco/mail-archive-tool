//go:build windows

package instance

import (
	"errors"
	"os"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

// defaultOwnerReader reads the file's owner SID (GetNamedSecurityInfo) and
// reports this account's: the process token's user SID, and its default owner SID
// (an elevated process creates files owned by Administrators, which is still this
// account's own file) [CC21].
func defaultOwnerReader(path string) (string, []string, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return "", nil, err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return "", nil, err
	}
	tok := windows.GetCurrentProcessToken()
	user, err := tok.GetTokenUser()
	if err != nil {
		return "", nil, err
	}
	accounts := []string{user.User.Sid.String()}
	if o, err := tokenDefaultOwner(tok); err == nil {
		accounts = append(accounts, o)
	}
	return owner.String(), accounts, nil
}

// tokenOwner is the TOKEN_OWNER structure GetTokenInformation(TokenOwner) fills.
type tokenOwner struct{ Owner *windows.SID }

// tokenDefaultOwner is the SID new objects created by this token are owned by.
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
	return o.String(), nil // the SID lives in b: read before b can be collected
}

// LogonSession is the Terminal Services session id of this process: a second
// sign-in of the same account (RDS, fast user switching) has another, and its
// browser cannot be opened from this one's [CC21].
func LogonSession() string {
	var id uint32
	if err := windows.ProcessIdToSessionId(uint32(os.Getpid()), &id); err != nil {
		return ""
	}
	return strconv.FormatUint(uint64(id), 10)
}
