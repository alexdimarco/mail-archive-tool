//go:build darwin

package app

import "golang.org/x/sys/unix"

// classifyView classifies how abs is reached from statfs's f_fstypename and
// f_mntfromname (`//user@server/share` for smbfs, `server:/path` for nfs, macFUSE's
// types for FUSE; design §4.7, RR-2b re-verification).
func classifyView(abs string) ceView {
	var sf unix.Statfs_t
	if err := unix.Statfs(abs, &sf); err != nil {
		return ceView{Kind: viewStacking}
	}
	return classifyFSName(cstr(sf.Fstypename[:]), cstr(sf.Mntfromname[:]))
}
