//go:build windows

package winsec

import (
	"golang.org/x/sys/windows"
)

// RestrictToOwner sets path's access list to this user alone, not inherited — the
// Windows form of chmod 600, for a file holding a secret such as a saved recovery
// key (design-encrypted-archives-phase3 §5.2) — and makes this user its owner. An
// administrator running elevated makes files Administrators own, so without the owner
// MailArchive's own secret file is one CheckSecretFile refuses, as not theirs (P3-8's
// CI run on windows-latest; T2-16). Where the owner cannot be set, the access list
// alone is, as before. Verified on a real Windows box in the Phase 3 Windows lab.
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
	if windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		tu.User.Sid, nil, dacl, nil) == nil {
		return nil
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
