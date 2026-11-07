//go:build darwin

package tbrestore

import "golang.org/x/sys/unix"

// OwnersIgnored reports a volume mounted with "Ignore ownership on this volume" (macOS
// MNT_IGNORE_OWNERSHIP, the default for an external drive): every account acts as the
// owner there, so an owner-only mode keeps no one out, though it reads back as owner-only
// (settled 2026-11-07 [R7-01]). A volume that cannot be read says false; the write's own
// read-back still holds then.
func OwnersIgnored(path string) bool {
	var st unix.Statfs_t
	if unix.Statfs(path, &st) != nil {
		return false
	}
	return st.Flags&unix.MNT_IGNORE_OWNERSHIP != 0
}
