//go:build windows

package schedule

import "mail-archive-tool/internal/winsec"

// locationWritableByOthersOS (Windows) is Q20's mechanism for this OS (MA-649): it
// reports whether dir's owner and DACL let an account other than the owner,
// SYSTEM, Administrators or CREATOR OWNER replace the scheduled program, via the
// shared winsec.PathWritableByOthers (GetNamedSecurityInfo + the pure DACL
// predicate). Fail-closed. Verified on a real Windows box, lab item L-W15
// (README-RMM.md §D).
func locationWritableByOthersOS(dir string) bool {
	return winsec.PathWritableByOthers(dir)
}
