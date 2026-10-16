//go:build !linux && !darwin && !windows

package app

import (
	"os"

	"golang.org/x/sys/unix"
)

// renameNoReplace on a unix without renameat2 or renameatx_np: the design's last
// resort, a hard link (which fails on an existing name), then the lookup and the
// handle-relative rename where links are unavailable.
func renameNoReplace(d *os.Root, oldn, newn string) error {
	f, err := d.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return placeFallback(d, f, oldn, newn)
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
	}); err != nil {
		return id, err
	}
	return id, ferr
}
