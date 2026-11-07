//go:build !windows && !darwin

package tbrestore

import "golang.org/x/sys/unix"

// fsType names the volume's file system for the kinds the restore must tell apart
// (FAT's 4 GiB file limit, design §4.7 note): "msdos" for FAT, "exfat", else "".
func fsType(path string) string {
	var st unix.Statfs_t
	if unix.Statfs(path, &st) != nil {
		return ""
	}
	switch uint64(st.Type) { //nolint:unconvert // the type differs by architecture
	case 0x4d44:
		return "msdos"
	case 0x2011bab0:
		return "exfat"
	}
	return ""
}
