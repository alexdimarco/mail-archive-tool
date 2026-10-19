//go:build darwin

package app

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// renameNoReplace gives oldn the name newn inside d without replacing anything:
// renameatx_np with RENAME_EXCL, relative to d's own handle (RR2-C10, never the
// path-based renamex_np).
func renameNoReplace(d *os.Root, oldn, newn string) error {
	f, err := d.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	var rerr error
	if err := rawControl(f, func(fd int) { rerr = unix.RenameatxNp(fd, oldn, fd, newn, unix.RENAME_EXCL) }); err != nil {
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

func readIdentity(f *os.File) (ceIdentity, error) {
	var id ceIdentity
	var ferr error
	if err := rawControl(f, func(fd int) {
		var st unix.Stat_t
		if e := unix.Fstat(fd, &st); e != nil {
			ferr = e
			return
		}
		id.Dev, id.Ino = uint64(st.Dev), st.Ino
		id.Birth = st.Btim.Nano()
		var sf unix.Statfs_t
		if e := unix.Fstatfs(fd, &sf); e == nil {
			id.FSType = cstr(sf.Fstypename[:])
		}
	}); err != nil {
		return id, err
	}
	return id, ferr
}
