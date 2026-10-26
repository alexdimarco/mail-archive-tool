//go:build windows

package keyring

import (
	"os"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/winsec"
)

// ownersOnly says why the open secret file f is not this account's alone, or "":
// its owner must be this user and no other account may read it (§3.4).
func ownersOnly(f *os.File, path string) string {
	if err := winsec.CheckSecretFile(windows.Handle(f.Fd())); err != nil {
		return err.Error() + ` — run icacls "` + path + `" /inheritance:r /grant:r "%USERNAME%:F"`
	}
	return ""
}
