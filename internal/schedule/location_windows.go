//go:build windows

package schedule

// windowsQ20Ready records whether lab item L-W15 has verified, on a real Windows
// box, the directory-DACL read that decides Q20 on Windows: that
// GetNamedSecurityInfo + the ACE walk correctly flag a folder another account can
// write (e.g. one inheriting Authenticated Users: Modify) and do NOT flag
// Program Files or a per-user folder. It defaults to false — the DACL mechanism
// (MA-649) is DORMANT until the lab confirms it, because a DACL walk written and
// "tested" only by cross-compiling on another OS is untrustworthy security code:
// shipping it active could wrongly refuse every legitimate install (fail-closed)
// or silently fail to protect (fail-open). While it is false, Q20 is not enforced
// on Windows and status/the install note say the Windows check is pending the lab.
// The lab builds the real read against live ACLs, exercises L-W15, and flips this.
const windowsQ20Ready = false

// locationWritableByOthersOS (Windows) is Q20's mechanism for this OS (MA-649).
// Until L-W15 flips windowsQ20Ready, it reports false (does not refuse): the
// world's-eye DACL read is built and verified in the §D lab round on a real box,
// not blind here. When the gate opens it reads dir's DACL through
// golang.org/x/sys/windows (GetNamedSecurityInfo, SE_FILE_OBJECT, DACL) and
// returns true when any ACE grants a write-class right to a principal other than
// the owner, SYSTEM or BUILTIN\Administrators.
func locationWritableByOthersOS(dir string) bool {
	if !windowsQ20Ready {
		return false
	}
	return windowsDACLWritableByOthers(dir)
}

// windowsDACLWritableByOthers is built in the §D lab round (L-W15) on a real
// Windows box, where the DACL read can be exercised against live ACLs. It is a
// stub here so the package cross-compiles for GOOS=windows; the gate above keeps
// it unreached until the lab replaces it and opens windowsQ20Ready.
func windowsDACLWritableByOthers(dir string) bool {
	return true // fail-closed placeholder; never reached while windowsQ20Ready is false
}
