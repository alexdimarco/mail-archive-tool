//go:build !windows

package keyring

import (
	"fmt"
	"os"
	"syscall"
)

// ownersOnly says why the open secret file f is not this account's alone, or "":
// it must be owned by the effective user and give group and others no access.
func ownersOnly(f *os.File, path string) string {
	fi, err := f.Stat()
	if err != nil {
		return fmt.Sprintf("cannot be checked: %v", err)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Geteuid() {
		return "belongs to another account — use a file of your own"
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Sprintf("can be read by other users (mode %04o) — run `chmod 600 %s`", fi.Mode().Perm(), path)
	}
	return ""
}
