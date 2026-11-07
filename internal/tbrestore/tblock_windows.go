//go:build windows

package tbrestore

import (
	"errors"
	"fmt"
	"io/fs"
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

var errHeldHere = errors.New("this process already holds the profile's lock")

// openProfileLock records the restore's intent; the lock is the handle take opens, and
// it enters the registry, by the file's identity, once that handle is open.
func openProfileLock(profile string) (*ProfileLock, error) {
	held.Lock()
	defer held.Unlock()
	if heldHere(profile) != nil {
		return nil, errHeldHere
	}
	return &ProfileLock{path: filepath.Join(profile, ParentLockName), profile: profile}, nil
}

// heldElsewhere is Windows' check A before the take: parent.lock opened with sharing
// fails while Thunderbird holds it (share 0). It is opened and closed at once; a
// Windows handle's close drops no lock of this process. A file that cannot be opened for
// another reason counts as held, with err why (MA-1095 lab BUG-2).
func (l *ProfileLock) heldElsewhere() (bool, error) { return windowsLockProbe(l.path) }

// lockHeldErr reports an error that means parent.lock is held: open by another program
// with no sharing (Thunderbird's share 0), or by this process.
func lockHeldErr(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, errHeldHere)
}

// lockConflictErr: on Windows the take is the open, so a conflict is a held lock.
func lockConflictErr(err error) bool { return lockHeldErr(err) }

// readOnlyErr reports a write refused because the medium is write-protected. (A
// read-only network share answers "access denied" instead: W-LOCKPERM's words name it.)
func readOnlyErr(err error) bool { return errors.Is(err, windows.ERROR_WRITE_PROTECT) }

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
		return &fs.PathError{Op: "open", Path: l.path, Err: errNotOrdinary}
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
		return errHeldHere
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

// probeLockFileErr is the plan's probe on Windows: held when parent.lock exists and
// cannot be opened for reading and writing with sharing (a sharing violation while
// Thunderbird holds it). It never creates the file. Closing a Windows handle drops no
// lock, so the probe never needs to answer self here (heldHere did).
func probeLockFileErr(profile string) (held_, self bool, _ error) {
	h, err := windowsLockProbe(filepath.Join(profile, ParentLockName))
	return h, false, err
}

// windowsLockProbe: no file, not held; a sharing or lock violation, held (err nil); any
// other failure to look at or open it, held in the safe direction, with err why (no hold
// was seen; MA-1095 lab BUG-2: a read-only share's "access denied" said "Thunderbird is
// open"). A file that is not an ordinary file is held.
func windowsLockProbe(p string) (bool, error) {
	fi, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if !fi.Mode().IsRegular() {
		// Settled 2026-11-07 [ADV-L2]: a folder or a link at parent.lock is never
		// Thunderbird's lock (it makes neither on Windows), so it is the "could not take
		// the lock" words, W-LOCKFAIL, at the plan and at Start, never "Thunderbird is open".
		return true, &fs.PathError{Op: "open", Path: p, Err: errNotOrdinary}
	}
	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if err != nil {
		if lockHeldErr(err) {
			return true, nil
		}
		return true, err
	}
	f.Close()
	return false, nil
}
