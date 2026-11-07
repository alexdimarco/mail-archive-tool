//go:build windows

package tbrestore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// ParentLockName is Thunderbird's profile lock file on Windows.
const ParentLockName = "parent.lock"

// ProfileLock is the restore's hold on a profile's Thunderbird lock on Windows:
// parent.lock opened exactly as Thunderbird opens it — read/write, no sharing — but
// OPEN_ALWAYS (made if absent, never truncated or written to), with
// FILE_FLAG_OPEN_REPARSE_POINT so a link planted at the name is opened as itself and
// refused, not followed (design §5.2 note item 1). The handle closes on Release and
// when the process ends, however it ends (lab B: nothing stale).
type ProfileLock struct {
	h       windows.Handle
	key     lockKey
	keyed   bool
	path    string
	profile string
	open    bool
}

// lockFileKey is the volume serial number and file index of the entry at p, read from a
// handle opened for its attributes only (which no sharing mode refuses, so a parent.lock
// held with share 0 can still be asked), the entry itself and never a link's target;
// ok false when there is none or it cannot be read.
func lockFileKey(p string) (lockKey, bool) {
	p16, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return lockKey{}, false
	}
	h, err := windows.CreateFile(p16, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return lockKey{}, false
	}
	defer windows.CloseHandle(h)
	return handleKey(h)
}

func handleKey(h windows.Handle) (lockKey, bool) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return lockKey{}, false
	}
	k := lockKey{volume: uint64(info.VolumeSerialNumber), file: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)}
	return k, k != lockKey{}
}

// openProfileLock records the restore's intent; the lock is the handle take opens, and
// it enters the registry, by the file's identity, once that handle is open.
func openProfileLock(profile string) (*ProfileLock, error) {
	held.Lock()
	defer held.Unlock()
	if heldHere(profile) != nil {
		return nil, errors.New("this process already holds the profile's lock")
	}
	return &ProfileLock{path: filepath.Join(profile, ParentLockName), profile: profile}, nil
}

// heldElsewhere is Windows' check A before the take: parent.lock opened with sharing
// fails while Thunderbird holds it (share 0). It is opened and closed at once; a
// Windows handle's close drops no lock of this process.
func (l *ProfileLock) heldElsewhere() bool { return windowsLockHeld(l.path) }

// take opens parent.lock with no sharing: the lock is the handle.
func (l *ProfileLock) take() error {
	p, err := windows.UTF16PtrFromString(l.path)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
		windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		windows.CloseHandle(h)
		return err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		windows.CloseHandle(h)
		return fmt.Errorf("%s is not an ordinary file", l.path)
	}
	key, ok := handleKey(h)
	if !ok {
		windows.CloseHandle(h)
		return fmt.Errorf("%s has no file identity", l.path)
	}
	held.Lock()
	defer held.Unlock()
	if held.m[key] != nil {
		windows.CloseHandle(h) // a Windows handle's close drops no lock of this process
		return errors.New("this process already holds the profile's lock")
	}
	l.h, l.open, l.key, l.keyed = h, true, key, true
	held.m[key] = l
	return nil
}

// Release closes the handle and leaves parent.lock in place, as Thunderbird leaves
// it. It is safe to call more than once.
func (l *ProfileLock) Release() {
	if l == nil {
		return
	}
	held.Lock()
	defer held.Unlock()
	if l.open {
		windows.CloseHandle(l.h)
		l.open = false
	}
	if l.keyed && held.m[l.key] == l {
		delete(held.m, l.key)
	}
}

// probeLockFile is the plan's probe on Windows: held when parent.lock exists and
// cannot be opened for reading and writing with sharing (a sharing violation while
// Thunderbird holds it). It never creates the file. Closing a Windows handle drops no
// lock, so the probe never needs to answer self here (heldHere did).
func probeLockFile(profile string) (held_, self bool) {
	return windowsLockHeld(filepath.Join(profile, ParentLockName)), false
}

func windowsLockHeld(p string) bool {
	fi, err := os.Lstat(p)
	if err != nil {
		return !errors.Is(err, os.ErrNotExist)
	}
	if !fi.Mode().IsRegular() {
		return true
	}
	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if err != nil {
		return true
	}
	f.Close()
	return false
}
