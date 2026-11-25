//go:build !windows

package wizard

import (
	"os"
	"syscall"
)

// setUID is the user the wizard runs as, whose folders §7's child read reads (a test stands in
// another user, as fault injection: a folder no test can chown).
var setUID = os.Geteuid

// setChildMine reports whether the folder open as f is the person's own: owned by the user the
// wizard runs as and writable by no one else (no group or other write bit). It reads the open
// folder, never its path again.
func setChildMine(f *os.File, fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == setUID() && fi.Mode().Perm()&0o022 == 0
}

// setVolumeKeepsNoAccessLists: never here. A unix system mounts a FAT or exFAT drive with an owner
// and mode bits for every folder, so its folders are judged by them as any other (the re-verify of
// rounds A and B checked a real exFAT image on macOS: a re-run brought every archive up to date).
func setVolumeKeepsNoAccessLists(*os.File) bool { return false }

// setForeign reports a folder Lstat found to be someone else's, or writable by someone else, before
// it is opened (setChildMine checks the open folder again).
func setForeign(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return !ok || int(st.Uid) != setUID() || fi.Mode().Perm()&0o022 != 0
}

// setRefusedIsForeign: here an open this user is refused is not taken as someone else's folder (the
// owner was already judged from Lstat), so it is cannot-read-now.
const setRefusedIsForeign = false

// setReparse reports a Windows reparse point (a junction, a mount folder); none here.
func setReparse(os.FileInfo) bool { return false }
