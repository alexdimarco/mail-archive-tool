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

// setReparse reports a Windows reparse point (a junction, a mount folder); none here.
func setReparse(os.FileInfo) bool { return false }
