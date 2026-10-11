//go:build !windows

package app

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// rawControl runs fn with f's descriptor held.
func rawControl(f *os.File, fn func(fd int)) error {
	sc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	return sc.Control(func(fd uintptr) { fn(int(fd)) })
}

// existsErr is the error a no-replace placement returns for a name that is there,
// as os.Rename reports it, so errors.Is(err, fs.ErrExist) holds.
func existsErr(oldn, newn string) error {
	return &os.LinkError{Op: "rename", Old: oldn, New: newn, Err: unix.EEXIST}
}

// placeFallback gives oldn the name newn without replacing anything where the
// filesystem takes neither renameat2's NOREPLACE nor renamex_np's EXCL. The order is
// the design's (§4.9): a hard link, which fails on an existing name, then the temp
// name is removed. Where hard links are unavailable too (FAT and exFAT), the name is
// looked up through the same directory handle and, if it is free, the handle-relative
// rename follows: a window in which a co-writer could make the name remains there,
// and it is the only way a FAT stick gets any message (a stated deviation).
func placeFallback(d *os.Root, f *os.File, oldn, newn string) error {
	err := d.Link(oldn, newn)
	if err == nil {
		return d.Remove(oldn)
	}
	if isExist(err) {
		return err
	}
	var ferr error
	cerr := rawControl(f, func(fd int) {
		var st unix.Stat_t
		switch e := unix.Fstatat(fd, newn, &st, unix.AT_SYMLINK_NOFOLLOW); {
		case e == nil:
			ferr = existsErr(oldn, newn)
		case errors.Is(e, unix.ENOENT):
			if e := unix.Renameat(fd, oldn, fd, newn); e != nil {
				ferr = &os.LinkError{Op: "rename", Old: oldn, New: newn, Err: e}
			}
		default:
			ferr = &os.LinkError{Op: "rename", Old: oldn, New: newn, Err: e}
		}
	})
	if cerr != nil {
		return cerr
	}
	return ferr
}

// noFlagSupport reports whether err says the filesystem does not take a rename flag.
func noFlagSupport(err error) bool {
	return errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) ||
		errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP)
}

func isReparsePoint(os.FileInfo) bool { return false }

// cstr is a NUL-terminated C char array as a string.
func cstr[T ~int8 | ~uint8](b []T) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		out = append(out, byte(c))
	}
	return string(out)
}
