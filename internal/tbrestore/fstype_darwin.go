//go:build darwin

package tbrestore

import "golang.org/x/sys/unix"

// fsType is the volume's file system name ("apfs", "hfs", "msdos", "exfat"), "" when
// it cannot be read.
func fsType(path string) string {
	var st unix.Statfs_t
	if unix.Statfs(path, &st) != nil {
		return ""
	}
	b := st.Fstypename[:]
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = byte(b[i])
	}
	return string(out)
}
