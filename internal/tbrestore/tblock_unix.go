//go:build !windows

package tbrestore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"mail-archive-tool/internal/thunderbird"
)

// ParentLockName is Thunderbird's profile lock file on macOS and Linux.
const ParentLockName = ".parentlock"

// ProfileLock is the restore's hold on a profile's Thunderbird lock: .parentlock,
// opened once (created only when absent, never written to), and a POSIX write record
// lock on the whole file, non-blocking — F_OFD_SETLK on Linux (it conflicts with
// Mozilla's F_SETLK and is not dropped when another descriptor of the file closes),
// F_SETLK on macOS and the other Unixes (the only kind there; see the sole-opener rule
// in tblock.go). Never the lockfile package (flock, which Linux keeps apart from fcntl
// locks, and which writes a holder line), and never a `lock` symlink.
type ProfileLock struct {
	f       *os.File
	key     string
	profile string
}

// openProfileLock opens the profile's .parentlock once and records it in the
// registry, so no probe of this process opens it again while it is held.
func openProfileLock(profile string) (*ProfileLock, error) {
	held.Lock()
	defer held.Unlock()
	key := thunderbird.ProfileKey(profile)
	if _, ok := held.m[key]; ok {
		return nil, errors.New("this process already holds the profile's lock")
	}
	p := filepath.Join(profile, ParentLockName)
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err // a link at .parentlock (Mozilla's fallback lock) cannot be opened: running
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%s is not an ordinary file", p)
	}
	l := &ProfileLock{f: f, key: key, profile: profile}
	held.m[key] = l
	return l, nil
}

// heldElsewhere asks, through the lock's own handle and taking nothing, whether
// another process holds a lock on .parentlock (F_GETLK; F_OFD_GETLK on Linux). An
// answer that cannot be read counts as held.
func (l *ProfileLock) heldElsewhere() bool {
	fl := wholeFile(unix.F_WRLCK)
	if err := unix.FcntlFlock(l.f.Fd(), getLockCmd, &fl); err != nil {
		return true
	}
	return fl.Type != unix.F_UNLCK
}

// take takes the lock, non-blocking. Held by another process is an error.
func (l *ProfileLock) take() error {
	fl := wholeFile(unix.F_WRLCK)
	return unix.FcntlFlock(l.f.Fd(), setLockCmd, &fl)
}

// Release closes the one handle (which releases the lock) and leaves the file in
// place, as Thunderbird leaves it. It is safe to call more than once.
func (l *ProfileLock) Release() {
	if l == nil {
		return
	}
	held.Lock()
	defer held.Unlock()
	if l.f == nil {
		return
	}
	l.f.Close()
	l.f = nil
	if held.m[l.key] == l {
		delete(held.m, l.key)
	}
}

func wholeFile(typ int16) unix.Flock_t {
	return unix.Flock_t{Type: typ, Whence: 0, Start: 0, Len: 0}
}

// probeLockFile is the plan's probe of a profile this process does not hold (held's
// mutex is held by the caller): .parentlock opened read-only and never created (the
// plan writes nothing), F_GETLK asked, the handle closed. No file: not held. A file
// that cannot be opened or asked counts as held.
func probeLockFile(profile string) bool {
	p := filepath.Join(profile, ParentLockName)
	fi, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		return true
	}
	if !fi.Mode().IsRegular() {
		return false // a link is symlinkLockHeld's to judge
	}
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return true
	}
	defer f.Close()
	fl := wholeFile(unix.F_WRLCK)
	if err := unix.FcntlFlock(f.Fd(), getLockCmd, &fl); err != nil {
		return true
	}
	return fl.Type != unix.F_UNLCK
}
