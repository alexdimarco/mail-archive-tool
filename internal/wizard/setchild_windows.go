//go:build windows

package wizard

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/winsec"
)

// setUID stands in for the user on unix only; on Windows the user is the process token's.
var setUID = os.Geteuid

// setChildMine reports whether the folder open as f is the person's own: owned by the user the
// wizard runs as, and with no allow entry that lets any other account than the owner, SYSTEM,
// Administrators or CREATOR OWNER write to it (winsec.DACLWritableByOthers). It reads the security
// descriptor of the open handle, never the path again; one it cannot read is not the person's.
func setChildMine(f *os.File, _ os.FileInfo) bool {
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !winsec.UserEntry(tu.User.Sid.String())(owner.String()) {
		return false
	}
	return !winsec.DACLWritableByOthers(sd.String(), owner.String())
}

// setForeign: Lstat says nothing of a folder's owner here; setChildMine judges the open folder.
func setForeign(os.FileInfo) bool { return false }

// setRefusedIsForeign: a folder this user is refused opening is someone else's here (the owner
// cannot be read without opening it), never read, never written into.
const setRefusedIsForeign = true

// setReparse reports a reparse point (a junction, a mount folder, a link), which is never read nor
// written into.
func setReparse(fi os.FileInfo) bool {
	if a, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		return a.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
	}
	return false
}
