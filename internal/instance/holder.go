package instance

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mail-archive-tool/internal/lockfile"
)

// Holder is the lock a desktop instance holds for its life, in Dir, and the
// instance file published while it holds it. Whoever holds the lock is the
// account's only instance of this configuration (P8).
type Holder struct {
	Dir     string
	Session string

	lock *lockfile.Lock
	info *Info
	stop chan struct{}
}

// Names are the lock and instance file names of a logon session: the account's
// own for session "" (and for the one session a POSIX login has), and one pair
// per Terminal Services session otherwise, so a second sign-in of the same
// account on the same computer (RDS, fast user switching) gets an instance of its
// own, whose browser opens in its own session, instead of a handoff that opens
// one in a session its user cannot see [CC21].
func Names(session string) (lock, file string) {
	if session == "" {
		return LockName, FileName
	}
	return "desktop-s" + session + ".lock", "desktop-instance-s" + session + ".json"
}

// Acquire takes the per-account lock of the logon session without waiting,
// creating dir 0700. When another process holds it the error wraps
// lockfile.ErrHeld: an instance exists or is starting.
func Acquire(dir, session string) (*Holder, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lockName, _ := Names(session)
	l, err := lockfile.AcquireAs(filepath.Join(dir, lockName), "desktop")
	if err != nil {
		return nil, err
	}
	return &Holder{Dir: dir, Session: session, lock: l}, nil
}

// IsHeld reports whether err says another process holds the lock.
func IsHeld(err error) bool { return errors.Is(err, lockfile.ErrHeld) }

// LockIsHeld reports whether another process holds the lock of the session in
// dir, by taking it and letting go at once; it touches no instance file.
func LockIsHeld(dir, session string) bool {
	lockName, _ := Names(session)
	l, err := lockfile.AcquireAs(filepath.Join(dir, lockName), "desktop")
	if err != nil {
		return errors.Is(err, lockfile.ErrHeld)
	}
	l.Release()
	return false
}

// FilePath is the instance file's path.
func (h *Holder) FilePath() string { return FilePathIn(h.Dir, h.Session) }

// FilePathIn is the instance file of the session in dir.
func FilePathIn(dir, session string) string {
	_, file := Names(session)
	return filepath.Join(dir, file)
}

// RemoveStale removes any instance file left by an earlier run: the lock holder
// owns the name now, so what is there is stale by definition. It is done right
// after taking the lock and before binding [CC21].
func (h *Holder) RemoveStale() { os.Remove(h.FilePath()) }

// Publish writes the instance file and remembers it, so Watch can rewrite it.
func (h *Holder) Publish(info Info) error {
	h.info = &info
	return Write(h.FilePath(), info)
}

// Watch re-checks the lock every interval (lockfile.StillHeld) and rewrites the
// instance file when it has gone missing — a purge, a clean-up, a user's tidy —
// so a later launch's retry finds it [CC21]. A lock that is no longer ours is
// reported to log and the file is left alone: another process owns the name.
// The returned func stops it.
func (h *Holder) Watch(interval time.Duration, log func(string)) (stop func()) {
	h.stop = make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-h.stop:
				return
			case <-t.C:
				if err := h.lock.StillHeld(); err != nil {
					log(err.Error())
					continue
				}
				if _, err := os.Lstat(h.FilePath()); err != nil && h.info != nil {
					if err := Write(h.FilePath(), *h.info); err != nil {
						log("could not rewrite the instance file: " + err.Error())
					}
				}
			}
		}
	}()
	return func() {
		select {
		case <-h.stop:
		default:
			close(h.stop)
		}
		<-done
	}
}

// StillHeld verifies the lock is still this process's.
func (h *Holder) StillHeld() error { return h.lock.StillHeld() }

// Close removes the instance file (best effort) and releases the lock.
func (h *Holder) Close() {
	if h == nil {
		return
	}
	if h.stop != nil {
		select {
		case <-h.stop:
		default:
			close(h.stop)
		}
	}
	os.Remove(h.FilePath())
	h.lock.Release()
}

// HolderPID is the pid the lock's holder line records, or 0 when it cannot be
// read (a Windows lock is a byte-range lock the holder line may not be readable
// through). The line is untrusted content; only a plain integer is believed.
func HolderPID(dir, session string) int {
	lockName, _ := Names(session)
	for _, f := range strings.Fields(lockfile.Holder(filepath.Join(dir, lockName))) {
		if v, ok := strings.CutPrefix(f, "pid="); ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}
