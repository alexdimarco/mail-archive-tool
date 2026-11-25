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
func RestrictToOwner(path string) error { return restrictToOwner(path, "") }

// RestrictFolderToOwner is RestrictToOwner for a folder whose new files and folders
// inherit the same owner-only entry (OICI), so what Thunderbird later makes in a folder
// a restore made is the user's alone too (design-restore-thunderbird §9.6, R6-01).
func RestrictFolderToOwner(path string) error { return restrictToOwner(path, "OICI") }

// RestrictFolderToOwnerAndAdmins is RestrictFolderToOwner that keeps SYSTEM and Administrators too
// (ownerAndAdminsDACL): a protected access list, nothing inherited from the folder's parent, giving
// full control to this user, SYSTEM and the Administrators group, each inherited by what is made in
// the folder, and this user its owner. A per-mailbox archive's folder is restricted with it (round D,
// the lead's call revising the review's RV-05 fold): backup software that runs as SYSTEM can still
// copy the person's archives, and no other ordinary account can change the folder, so the set's own
// check of a folder (setChildMine, which accepts SYSTEM and Administrators) still reads it as the
// person's (INS-01 stays fixed).
func RestrictFolderToOwnerAndAdmins(path string) error { return restrictTo(path, ownerAndAdminsDACL) }

func restrictToOwner(path, inherit string) error {
	return restrictTo(path, func(user string) string { return "D:P(A;" + inherit + ";FA;;;" + user + ")" })
}

// restrictTo sets path's access list to the one daclFor gives for this user's SID (SDDL), and makes
// this user its owner (where the owner cannot be set, the access list alone).
func restrictTo(path string, daclFor func(user string) string) error {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString(daclFor(tu.User.Sid.String()))
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
