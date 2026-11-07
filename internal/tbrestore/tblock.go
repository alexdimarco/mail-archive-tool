package tbrestore

import (
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
// someone holds the profile's Thunderbird lock; self when that is this process.
func parentLockProbe(profile string) (held_, self bool) {
	held.Lock()
	defer held.Unlock()
	if heldHere(profile) != nil {
		return true, true
	}
	return probeLockFile(profile)
}

// TakeProfileLock is the Start sequence of §5.1 and §5.3 (as folded): checks A and B
// (CheckRunning, which creates nothing); then open the profile's Thunderbird lock file
// once (created only when absent, never written to) and ask A again through that very
// handle; take the lock; then check B once more. Any signal, or a failed take, is
// W-RUNNING and the lock is released. A signal before the open leaves no lock file
// made.
func TakeProfileLock(profile string) (*ProfileLock, error) {
	if CheckRunning(profile).Running() {
		return nil, refuse("W-RUNNING", RunningWords(), nil)
	}
	l, err := openProfileLock(profile)
	if err != nil {
		return nil, refuse("W-RUNNING", RunningWords(), err)
	}
	if symlinkLockHeld(profile) || l.heldElsewhere() || ProcessRunning() {
		l.Release()
		return nil, refuse("W-RUNNING", RunningWords(), nil)
	}
	if err := l.take(); err != nil {
		l.Release()
		return nil, refuse("W-RUNNING", RunningWords(), err)
	}
	if ProcessRunning() {
		l.Release()
		return nil, refuse("W-RUNNING", RunningWords(), nil)
	}
	return l, nil
}
