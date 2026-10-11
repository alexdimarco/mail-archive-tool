//go:build linux

package app

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// renameNoReplace gives oldn the name newn inside d without replacing anything:
// renameat2 with RENAME_NOREPLACE, relative to d's own handle (RR2-C10).
func renameNoReplace(d *os.Root, oldn, newn string) error {
	f, err := d.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	var rerr error
	if err := rawControl(f, func(fd int) { rerr = unix.Renameat2(fd, oldn, fd, newn, unix.RENAME_NOREPLACE) }); err != nil {
		return err
	}
	switch {
	case rerr == nil:
		return nil
	case errors.Is(rerr, unix.EEXIST):
		return existsErr(oldn, newn)
	case noFlagSupport(rerr):
		return placeFallback(d, f, oldn, newn)
	}
	return &os.LinkError{Op: "rename", Old: oldn, New: newn, Err: rerr}
}

// linuxFSNames are the statfs f_type magic numbers of the filesystems the export
// cares about; any other is its number.
var linuxFSNames = map[int64]string{
	0x4d44: "vfat", 0x2011BAB0: "exfat", 0x5346544e: "ntfs", 0x65735546: "fuseblk", 0xEF53: "ext4",
	0x58465342: "xfs", 0x9123683E: "btrfs", 0x01021994: "tmpfs", 0x6969: "nfs", 0xFF534D42: "cifs",
	0xFE534D42: "smb2", 0x794c7630: "overlay", 0x2fc12fc1: "zfs", 0x3153464a: "jfs", 0xF2F52010: "f2fs",
}

func readIdentity(f *os.File) (ceIdentity, error) {
	var id ceIdentity
	var ferr error
	if err := rawControl(f, func(fd int) {
		var st unix.Stat_t
		if e := unix.Fstat(fd, &st); e != nil {
			ferr = e
			return
		}
		id.Dev, id.Ino = uint64(st.Dev), uint64(st.Ino)
		var sf unix.Statfs_t
		if e := unix.Fstatfs(fd, &sf); e == nil {
			t := int64(sf.Type)
			if name, ok := linuxFSNames[t]; ok {
				id.FSType = name
			} else {
				id.FSType = fmt.Sprintf("0x%x", t)
			}
		}
		var stx unix.Statx_t
		if e := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_BTIME, &stx); e == nil && stx.Mask&unix.STATX_BTIME != 0 {
			id.Birth = int64(stx.Btime.Sec)*1e9 + int64(stx.Btime.Nsec)
		}
	}); err != nil {
		return id, err
	}
	return id, ferr
}
