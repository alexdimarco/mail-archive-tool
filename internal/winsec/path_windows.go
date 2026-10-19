//go:build windows

package winsec

import "golang.org/x/sys/windows"

// PathWritableByOthers reads path's owner and DACL through GetNamedSecurityInfo
// (SE_FILE_OBJECT covers both files and directories) and reports whether the DACL
// grants a write-class right to any account other than the owner, SYSTEM,
// Administrators or CREATOR OWNER. Fail-closed: a descriptor it cannot read counts
// as writable (refuse rather than wave through). The read is verified on a real
// Windows box, lab item L-W15 (README-RMM.md §D); the decision it feeds is
// unit-tested on any OS through DACLWritableByOthers.
func PathWritableByOthers(path string) bool {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return true
	}
	owner := ""
	if o, _, e := sd.Owner(); e == nil && o != nil {
		owner = o.String()
	}
	return DACLWritableByOthers(sd.String(), owner)
}
