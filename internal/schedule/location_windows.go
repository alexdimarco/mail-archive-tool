//go:build windows

package schedule

import "golang.org/x/sys/windows"

// locationWritableByOthersOS (Windows) is Q20's mechanism for this OS (MA-649): it
// reads dir's owner and DACL through GetNamedSecurityInfo and asks the pure
// sddlDACLWritableByOthers whether the DACL grants a write-class right to any
// principal other than the owner, SYSTEM, Administrators or CREATOR OWNER — e.g.
// a folder inheriting Authenticated Users: Modify. Fail-closed: a descriptor it
// cannot read counts as writable (refuse rather than wave through). The decision
// logic is unit-tested on any OS (location_sddl_test.go); this read is verified
// on a real Windows box, lab item L-W15 (README-RMM.md §D).
func locationWritableByOthersOS(dir string) bool {
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return true
	}
	owner := ""
	if o, _, e := sd.Owner(); e == nil && o != nil {
		owner = o.String()
	}
	return sddlDACLWritableByOthers(sd.String(), owner)
}
