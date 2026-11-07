//go:build !windows && !darwin

package tbrestore

import "os"

// mkdirPrivate and createPrivate (Linux and the other Unixes; settled 2026-11-07
// [R7-01]): mkdirat at 0700 and openat at 0600 through the root are owner-only at birth.
// The umask can only narrow those modes, and a default POSIX access list Local Folders
// carries is masked by them: its group-class entries get the mode's group bits, which are
// zero. FS.Private then reads each back.
func mkdirPrivate(root *os.Root, name string) error { return root.Mkdir(name, 0o700) }

func createPrivate(root *os.Root, name string) (wfile, error) {
	return root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}
