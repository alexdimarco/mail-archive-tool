//go:build !windows

package tbrestore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
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
	key     lockKey
	profile string
	// kept are descriptors of the held file that a probe or a second open of this
	// process came to hold by a race (the name swapped for the held file between its
	// identity check and its open): closing one would drop the lock on macOS, so each is
	// kept open until Release (the sole-opener rule's last gap, ADV-01).
	kept []*os.File
}

var errHeldHere = errors.New("this process already holds the profile's lock")

// lockFileKey is the identity of the entry at p, read by Lstat (never opened, a link
// judged as itself); ok false when there is none or it cannot be read.
func lockFileKey(p string) (lockKey, bool) {
	fi, err := os.Lstat(p)
	if err != nil {
		return lockKey{}, false
	}
	return infoKey(fi)
}

func infoKey(fi os.FileInfo) (lockKey, bool) {
	id, err := infoIdentity(fi)
	if err != nil {
		return lockKey{}, false
	}
	return lockKey{volume: id.Volume, file: id.FileID}, true
}

// keepIfHeld is the race's answer after an open: when the descriptor f is of a file
// this process holds a lock on, f is kept open with that lock (never closed before
// Release) and the lock is returned. The caller holds held's mutex.
func keepIfHeld(f *os.File) *ProfileLock {
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	k, ok := infoKey(fi)
	if !ok {
		return nil
	}
	l := held.m[k]
	if l != nil {
		l.kept = append(l.kept, f)
	}
	return l
}

// openProfileLock opens the profile's .parentlock once and records it in the
// registry by the file's identity, so no probe of this process opens it again while it
// is held, however the profile is spelled or whichever hard link reaches it.
func openProfileLock(profile string) (*ProfileLock, error) {
	held.Lock()
	defer held.Unlock()
	if heldHere(profile) != nil {
		return nil, errHeldHere
	}
	p := filepath.Join(profile, ParentLockName)
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err // a link at .parentlock (Mozilla's fallback lock) cannot be opened: running
	}
	if keepIfHeld(f) != nil {
		return nil, errHeldHere
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, &fs.PathError{Op: "open", Path: p, Err: errNotOrdinary}
	}
	key, ok := infoKey(fi)
	if !ok {
		f.Close()
		return nil, fmt.Errorf("%s has no file identity", p)
	}
	l := &ProfileLock{f: f, key: key, profile: profile}
	held.m[key] = l
	return l, nil
}

// heldElsewhere asks, through the lock's own handle and taking nothing, whether
// another process holds a lock on .parentlock (F_GETLK; F_OFD_GETLK on Linux). An
// answer that cannot be read counts as held, with err why (no hold was seen).
func (l *ProfileLock) heldElsewhere() (bool, error) {
	fl := wholeFile(unix.F_WRLCK)
	if err := unix.FcntlFlock(l.f.Fd(), getLockCmd, &fl); err != nil {
		return true, err
	}
	return fl.Type != unix.F_UNLCK, nil
}

// lockHeldErr reports an open error that means the lock is held: this process's own
// (errHeldHere), or a link at .parentlock (O_NOFOLLOW's ELOOP), which is Mozilla's
// fallback lock (MA-1095 lab BUG-2).
func lockHeldErr(err error) bool { return errors.Is(err, errHeldHere) || errors.Is(err, unix.ELOOP) }

// lockConflictErr reports a take refused because another process holds a conflicting
// lock: F_SETLK's EAGAIN or EACCES (POSIX allows either), or this process's own.
func lockConflictErr(err error) bool {
	return errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EACCES) || errors.Is(err, errHeldHere)
}

// readOnlyErr reports a write refused because the file system is read-only.
func readOnlyErr(err error) bool { return errors.Is(err, unix.EROFS) }

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
	for _, k := range l.kept {
		k.Close()
	}
	l.kept = nil
	if held.m[l.key] == l {
		delete(held.m, l.key)
	}
}

func wholeFile(typ int16) unix.Flock_t {
	return unix.Flock_t{Type: typ, Whence: 0, Start: 0, Len: 0}
}

// probeLockFile is the plan's probe of a lock file this process does not hold (held's
// mutex is held by the caller, and heldHere found no lock on it): .parentlock opened
// read-only and never created (the plan writes nothing), F_GETLK asked, the handle
// closed. No file: not held. A file that cannot be opened or asked counts as held, with
// err why (no hold was seen; MA-1095 lab BUG-2). If the open landed on a file this
// process holds after all (the name swapped in between), the descriptor is kept with
// that lock, never closed, and the answer is self.
func probeLockFileErr(profile string) (held_, self bool, _ error) {
	p := filepath.Join(profile, ParentLockName)
	fi, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return true, false, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return false, false, nil // a link is symlinkLockHeld's and staleParentLink's to judge
	}
	if !fi.Mode().IsRegular() {
		// A folder (or other non-file) at the lock name: worded at the plan as at Start,
		// W-LOCKFAIL (settled 2026-11-07 [ADV-L1, ADV-L2]; it was "closed" here before).
		return true, false, &fs.PathError{Op: "open", Path: p, Err: errNotOrdinary}
	}
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return true, false, err
	}
	if keepIfHeld(f) != nil {
		return true, true, nil
	}
	defer f.Close()
	fl := wholeFile(unix.F_WRLCK)
	if err := unix.FcntlFlock(f.Fd(), getLockCmd, &fl); err != nil {
		return true, false, err
	}
	return fl.Type != unix.F_UNLCK, false, nil
}
