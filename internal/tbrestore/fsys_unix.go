//go:build !windows

package tbrestore

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func syncDirFile(f *os.File) error { return f.Sync() }

// isSharingViolation: there is no sharing violation outside Windows; a busy file
// (EBUSY, ETXTBSY) is treated alike.
func isSharingViolation(err error) bool {
	return errors.Is(err, syscall.EBUSY) || errors.Is(err, syscall.ETXTBSY)
}

// removeSeenAt is RemoveSeen on macOS and Linux: each folder on the way opened with
// openat(O_DIRECTORY|O_NOFOLLOW) relative to the one before it (a link anywhere refuses
// the open) and its fstat compared with the scan's; then the entry fstatat-ed with
// AT_SYMLINK_NOFOLLOW relative to its checked parent, compared, and unlinkat-ed relative
// to that same parent. The one window left is between that fstatat and unlinkat, and it
// can only reach a name inside the checked folder; unlinkat never follows a link there
// (design §9 ADV-02 note).
func removeSeenAt(top *os.File, parts []string, want []fs.FileInfo) error {
	cur := int(top.Fd())
	var opened []int
	defer func() {
		for _, fd := range opened {
			unix.Close(fd)
		}
	}()
	for i, p := range parts[:len(parts)-1] {
		fd, err := unix.Openat(cur, p, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			if errors.Is(err, unix.ENOENT) {
				return &os.PathError{Op: "openat", Path: strings.Join(parts[:i+1], "/"), Err: err}
			}
			return errChanged // a link (ELOOP) or not a folder (ENOTDIR) now
		}
		opened = append(opened, fd)
		var st unix.Stat_t
		if err := unix.Fstat(fd, &st); err != nil || !sameStat(want[i], &st) {
			return errChanged
		}
		cur = fd
	}
	name := strings.Join(parts, "/")
	last := parts[len(parts)-1]
	var st unix.Stat_t
	if err := unix.Fstatat(cur, last, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return &os.PathError{Op: "fstatat", Path: name, Err: err}
	}
	w := want[len(want)-1]
	if !sameStat(w, &st) {
		return errChanged
	}
	beforeUnlinkSeam(name)
	flag := 0
	if w.IsDir() {
		flag = unix.AT_REMOVEDIR
	}
	if err := unix.Unlinkat(cur, last, flag); err != nil {
		return &os.PathError{Op: "unlinkat", Path: name, Err: err}
	}
	return nil
}

// sameStat reports a fresh stat is the scan's entry: the same device and inode, a folder
// for a folder and a regular file for a file (a link is neither).
func sameStat(want fs.FileInfo, st *unix.Stat_t) bool {
	id, err := infoIdentity(want)
	if err != nil {
		return false
	}
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		if !want.IsDir() {
			return false
		}
	case unix.S_IFREG:
		if !want.Mode().IsRegular() {
			return false
		}
	default:
		return false
	}
	return id.Volume == uint64(st.Dev) && id.FileID == uint64(st.Ino) //nolint:unconvert // the types differ by OS
}
