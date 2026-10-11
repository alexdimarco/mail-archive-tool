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
	Dir string

	lock *lockfile.Lock
	info *Info
	stop chan struct{}
}

// Acquire takes the per-account lock without waiting, creating dir 0700. When
// another process holds it the error wraps lockfile.ErrHeld: an instance exists
// or is starting.
func Acquire(dir string) (*Holder, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	l, err := lockfile.AcquireAs(filepath.Join(dir, LockName), "desktop")
	if err != nil {
		return nil, err
	}
	return &Holder{Dir: dir, lock: l}, nil
}

// IsHeld reports whether err says another process holds the lock.
func IsHeld(err error) bool { return errors.Is(err, lockfile.ErrHeld) }

// FilePath is the instance file's path.
func (h *Holder) FilePath() string { return filepath.Join(h.Dir, FileName) }

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
func HolderPID(dir string) int {
	for _, f := range strings.Fields(lockfile.Holder(filepath.Join(dir, LockName))) {
		if v, ok := strings.CutPrefix(f, "pid="); ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}
