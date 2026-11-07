package tbrestore

import (
	"errors"
	"io/fs"
	"path/filepath"
	"sync"
)

// Thunderbird's own profile lock, as the restore holds it while it writes (design
// §5.2 note items 1, 2 and 4). Mozilla's nsProfileLock.cpp (read 2026-11-07 on the
// gecko-dev mirror, toolkit/profile/nsProfileLock.cpp) takes, on macOS and Linux, a
// non-blocking whole-file F_WRLCK fcntl lock (F_SETLK after an F_GETLK) on
// .parentlock, which it opens O_WRONLY|O_CREAT|O_TRUNC; on macOS it falls back to a
// symlink at .parentlock only when that fcntl fails for a reason other than
// access-denied, which is why the Mac lab saw no `lock` symlink; on Linux, after the
// fcntl succeeds, it also makes the old-style `lock` symlink. On Windows it opens
// parent.lock with CreateFileW(GENERIC_READ|GENERIC_WRITE, share 0, CREATE_ALWAYS).
// So the restore's lock conflicts with Thunderbird's both ways (Mac and Linux lab of
// 2026-11-06, Windows lab B), and it never writes the file (it opens with OPEN_ALWAYS
// on Windows and without O_TRUNC elsewhere).
//
// The sole-opener rule (lead's call 2026-11-07, from the Mac lab): on macOS a POSIX
// record lock belongs to the process, and closing ANY descriptor of the file in that
// process drops it — the lab saw a second handle opened and closed silently let
// Thunderbird in. So a profile's .parentlock is opened by the restore once, by its
// ProfileLock, and held to the end; every probe of a profile this process has a
// ProfileLock on answers from this registry and never opens the file. The registry's
// mutex also covers each probe's own open and close, so no probe can close a handle
// while another goroutine of this process holds the lock.

// ThunderbirdRefusalFile is the one file a Thunderbird refused by the profile lock
// writes into the profile, on macOS and Linux (Mac and Linux lab of 2026-11-06: a
// counter of failed lock attempts, content "2" after two refusals). It is
// Thunderbird's, never the restore's, and outside Local Folders: R40's "only adds"
// holds of what MailArchive writes, so a test or lab row that holds the profile
// unchanged across a refused Thunderbird allows this one file (lead's call
// 2026-11-07).
const ThunderbirdRefusalFile = "Telemetry.FailedProfileLocks.txt"

// lockKey is a lock file's identity: device and inode on macOS and Linux, volume serial
// number and file index on Windows. The registry is keyed by it, not by the profile's
// path, so the same lock file reached by another spelling (macOS's /System/Volumes/Data
// firmlink) or through another profile folder whose lock file is a hard link to it is
// still known as held here (ADV-01, settled 2026-11-07).
type lockKey struct{ volume, file uint64 }

var held = struct {
	sync.Mutex
	m map[lockKey]*ProfileLock
}{m: map[lockKey]*ProfileLock{}}

// heldHere is the ProfileLock this process has on the lock file at the profile's lock
// name, judged by that file's identity read without opening it for reading or writing
// (a link at the name is judged as itself, never followed); nil when none. The caller
// holds held's mutex.
func heldHere(profile string) *ProfileLock {
	k, ok := lockFileKey(filepath.Join(profile, ParentLockName))
	if !ok {
		return nil
	}
	return held.m[k]
}

// parentLockProbe is check A's lock-file part (CheckRunning): held is true when
// someone holds the profile's Thunderbird lock, or when the lock file cannot be opened or
// asked (the safe direction); self when the holder is this process; err, for that last
// case only, why the file could not be opened or asked (no hold was seen), so the words
// can say so (settled 2026-11-07 [MA-1095 lab BUG-2]).
func parentLockProbe(profile string) (held_, self bool, err error) {
	held.Lock()
	defer held.Unlock()
	if heldHere(profile) != nil {
		return true, true, nil
	}
	return probeLockFileErr(profile)
}

// probeLockFile is probeLockFileErr without the reason.
func probeLockFile(profile string) (held_, self bool) {
	h, s, _ := probeLockFileErr(profile)
	return h, s
}

// The words for a lock file that cannot be made, opened or asked although no one was
// seen holding it (settled 2026-11-07 [MA-1095 lab BUG-2]; the lead's words,
// owner-delegated). Until then every such failure said W-RUNNING, "Thunderbird is open",
// and Check again looped (the lab's read-only network share). Each still refuses, and
// nothing is written; only "Thunderbird is open" is kept for a lock actually held.
const (
	wLockPerm = "MailArchive is not allowed to make or open Thunderbird's lock file (FILE) in this profile's folder, so nothing was restored: the restore holds that lock to keep Thunderbird closed while it writes. This happens when the profile is on a read-only network share, or its folder belongs to another account. Choose a profile on this computer's own drive that your account can change, then click Check again."
	wLockRO   = "This profile's folder (PATH) is on a read-only drive, so MailArchive could not make Thunderbird's lock file there and nothing was restored: the restore holds that lock to keep Thunderbird closed while it writes. Choose a profile on a drive this computer can write to, then click Check again."
	wLockFail = "MailArchive could not take Thunderbird's lock on this profile (FILE: WHY), so nothing was restored. If Thunderbird is open, close it. Otherwise choose a profile on this computer's own drive, then click Check again."
	// wLockStale (W-LOCKFAIL), settled 2026-11-07 [ADV-L1]; the lead's call, these words
	// the builder's under the lead's owner-delegated wording authority.
	wLockStale = "Thunderbird's lock on this profile (FILE) is a link left behind when Thunderbird last closed without quitting normally, so nothing was restored. Open Thunderbird with this profile and quit it once, which clears the old lock, then click Check again. If this profile is also used from another computer, Thunderbird may be open there: close it there first."
	// wLockRemote (W-RUNNING), settled 2026-11-07 [PCT-2]; the lead's call, these words the
	// builder's under the lead's owner-delegated wording authority. A lock link made by
	// another computer: Thunderbird may be open there. Never "quit it once" (opening and
	// quitting Thunderbird here does not clear another computer's lock).
	wLockRemote = "Thunderbird's lock on this profile (FILE) was made by another computer, so nothing was restored. Thunderbird may be open on another computer that uses this profile: close it there, then click Check again."
)

// staleLinkRefusal is the refusal for a `.parentlock` link left behind (ADV-L1).
func staleLinkRefusal(profile string) error {
	return refuse("W-LOCKFAIL", fill(wLockStale, "FILE", filepath.Join(profile, ParentLockName)), nil)
}

// remoteLinkRefusal is the refusal for a lock link naming another computer (PCT-2): the
// link found, `.parentlock` when both names are links.
func remoteLinkRefusal(profile string) error {
	file := filepath.Join(profile, ParentLockName)
	if !remoteParentLink(profile) {
		file = filepath.Join(profile, "lock")
	}
	return refuse("W-RUNNING", fill(wLockRemote, "FILE", file), nil)
}

// errNotOrdinary is why a lock name that holds a folder, or a link that is not
// Mozilla's, cannot be Thunderbird's lock (settled 2026-11-07 [ADV-L1, ADV-L2]): worded
// W-LOCKFAIL at the plan's check and at Start alike, never "Thunderbird is open".
var errNotOrdinary = errors.New("it is a folder or a link, not an ordinary file")

// lockOpenRefusal is the refusal for err from making, opening or asking the profile's
// lock file: W-RUNNING only when err means the lock is held (lockHeldErr); otherwise the
// cause in words — read-only (W-LOCKRO), not allowed (W-LOCKPERM), or any other
// (W-LOCKFAIL, naming the error).
func lockOpenRefusal(profile string, err error) error {
	file := filepath.Join(profile, ParentLockName)
	switch {
	case lockHeldErr(err) && remoteParentLink(profile):
		return remoteLinkRefusal(profile) // the link appeared after the check (PCT-2)
	case lockHeldErr(err) && staleParentLink(profile):
		return staleLinkRefusal(profile) // the link appeared after the check (ADV-L1)
	case lockHeldErr(err):
		return refuse("W-RUNNING", RunningWords(), err)
	case readOnlyErr(err):
		return refuse("W-LOCKRO", fill(wLockRO, "PATH", profile), err)
	case errors.Is(err, fs.ErrPermission):
		return refuse("W-LOCKPERM", fill(wLockPerm, "FILE", file), err)
	}
	cause := err
	var pe *fs.PathError
	if errors.As(err, &pe) {
		cause = pe.Err // the file is named already
	}
	return refuse("W-LOCKFAIL", fill(wLockFail, "FILE", file, "WHY", why(cause)), err)
}

// lockTakeRefusal is the refusal for err from taking the lock: a conflict with another
// holder is W-RUNNING (lockConflictErr); anything else is worded as lockOpenRefusal does.
func lockTakeRefusal(profile string, err error) error {
	if lockConflictErr(err) {
		return refuse("W-RUNNING", RunningWords(), err)
	}
	return lockOpenRefusal(profile, err)
}

// RunningRefusal is the refusal for a profile CheckRunning found in use (s.Running()):
// W-RUNNING, except when the only signal was a lock file that could not be opened or asked
// (no hold seen, no Thunderbird process), which is worded by its cause (MA-1095 lab BUG-2),
// or a `.parentlock` link left behind (ADV-L1, settled 2026-11-07), worded as that. A
// Thunderbird process, or a hold seen, is W-RUNNING whatever else was found. Settled
// 2026-11-07 [PCT-2]: a lock link naming another computer, with no Thunderbird process
// here, is W-RUNNING in its own words (wLockRemote): it may be open on that computer.
func RunningRefusal(profile string, s RunningSignal) error {
	switch {
	case s.RemoteLink && !s.ByProcess:
		return remoteLinkRefusal(profile)
	case s.ByProcess || (s.ByLock && s.LockErr == nil):
		return refuse("W-RUNNING", RunningWords(), nil)
	case s.StaleLink:
		return staleLinkRefusal(profile)
	case s.LockErr != nil:
		return lockOpenRefusal(profile, s.LockErr)
	}
	return refuse("W-RUNNING", RunningWords(), nil)
}

// Binds reports whether the lock is on the lock file of the profile folder the plan
// pinned (R6-05, settled 2026-11-07): the folder at profile is still, by identity, id,
// and the lock file at its lock name is the very file this lock holds; read twice around
// the lock file's look, so a folder swapped in between is caught. A profile folder moved
// aside after the re-check, with a decoy at its path, gets the decoy's lock taken and is
// refused here, the lock released by the caller.
func (l *ProfileLock) Binds(profile string, id Identity) bool {
	if l == nil {
		return false
	}
	if now, err := DirIdentity(profile); err != nil || !now.Same(id) {
		return false
	}
	k, ok := lockFileKey(filepath.Join(profile, ParentLockName))
	if !ok || k != l.key {
		return false
	}
	now, err := DirIdentity(profile)
	return err == nil && now.Same(id)
}

// TakeProfileLock is the Start sequence of §5.1 and §5.3 (as folded): checks A and B
// (CheckRunning, which creates nothing); then open the profile's Thunderbird lock file
// once (created only when absent, never written to) and ask A again through that very
// handle; take the lock; then check B once more. Any signal, or a failed take, is
// W-RUNNING and the lock is released. A signal before the open leaves no lock file
// made. Settled 2026-11-07 [MA-1095 lab BUG-2]: a lock file that cannot be made, opened,
// asked or locked for another reason than a holder (read-only, not allowed, other) is
// refused in words naming that cause, never "Thunderbird is open".
func TakeProfileLock(profile string) (*ProfileLock, error) {
	if s := CheckRunning(profile); s.Running() {
		return nil, RunningRefusal(profile, s)
	}
	l, err := openProfileLock(profile)
	if err != nil {
		return nil, lockOpenRefusal(profile, err)
	}
	if sym, remote := symlinkLockHeld(profile); sym || ProcessRunning() {
		l.Release()
		return nil, RunningRefusal(profile, RunningSignal{ByLock: sym, RemoteLink: remote, ByProcess: ProcessRunning()})
	}
	if held, herr := l.heldElsewhere(); held {
		l.Release()
		if herr != nil {
			return nil, lockOpenRefusal(profile, herr)
		}
		return nil, refuse("W-RUNNING", RunningWords(), nil)
	}
	if err := l.take(); err != nil {
		l.Release()
		return nil, lockTakeRefusal(profile, err)
	}
	if ProcessRunning() {
		l.Release()
		return nil, refuse("W-RUNNING", RunningWords(), nil)
	}
	return l, nil
}
