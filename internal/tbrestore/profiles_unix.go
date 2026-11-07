//go:build !windows

package tbrestore

import (
	"os"
	"runtime"
	"syscall"
)

// profileBases: macOS ~/Library/Thunderbird; Linux and the other Unixes
// ~/.thunderbird, then the Snap location, then the Flatpak location (design §3.1
// step 1). The home folder is os.UserHomeDir's.
func profileBases() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return unixHomeBases(home, runtime.GOOS)
}

// profileModeOK is §3.1 step 3's macOS and Linux check: the folder is this user's
// and neither group- nor world-writable (new code on Linux, §2 note).
func profileModeOK(fi os.FileInfo) bool {
	if fi.Mode().Perm()&0o022 != 0 {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
