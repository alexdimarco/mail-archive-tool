//go:build windows

package tbrestore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/thunderbird"
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
	key     string
	path    string
	profile string
	open    bool
}

func openProfileLock(profile string) (*ProfileLock, error) {
	held.Lock()
	defer held.Unlock()
	key := thunderbird.ProfileKey(profile)
	if _, ok := held.m[key]; ok {
		return nil, errors.New("this process already holds the profile's lock")
	}
	l := &ProfileLock{key: key, path: filepath.Join(profile, ParentLockName), profile: profile}
	held.m[key] = l
	return l, nil
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
	held.Lock()
	l.h, l.open = h, true
	held.Unlock()
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
	if held.m[l.key] == l {
		delete(held.m, l.key)
	}
}

// probeLockFile is the plan's probe on Windows: held when parent.lock exists and
// cannot be opened for reading and writing with sharing (a sharing violation while
// Thunderbird holds it). It never creates the file.
func probeLockFile(profile string) bool {
	return windowsLockHeld(filepath.Join(profile, ParentLockName))
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
