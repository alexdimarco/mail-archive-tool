//go:build windows

package tbrestore

import (
	"errors"
	"os"
	"os/exec"

	"golang.org/x/sys/windows"
)

// linkKinds are the links a test can plant on Windows: a symbolic link (when the user
// may make one) and a junction (any user may).
func linkKinds() []string { return []string{"symlink", "junction"} }

// makeLink makes at link a symbolic link to rel, or a junction to the folder abs.
func makeLink(kind, link, abs, rel string) error {
	if kind == "symlink" {
		return os.Symlink(rel, link)
	}
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, abs).CombinedOutput()
	if err != nil {
		return &os.PathError{Op: "mklink " + string(out), Path: link, Err: err}
	}
	return nil
}

// keptHandles: Windows keeps no second handle (closing one drops no lock).
func keptHandles(*ProfileLock) int { return 0 }

// isNoSymlinkPrivilege: the VM's user cannot make symbolic links.
func isNoSymlinkPrivilege(err error) bool {
	return errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD)
}
