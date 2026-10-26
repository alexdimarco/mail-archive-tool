//go:build windows

package winsec

import (
	"golang.org/x/sys/windows"
)

// RestrictToOwner sets path's access list to this user alone, not inherited — the
// Windows form of chmod 600, for a file holding a secret such as a saved recovery
// key (design-encrypted-archives-phase3 §5.2). Verified on a real Windows box in
// the Phase 3 Windows lab.
func RestrictToOwner(path string) error {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + tu.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
