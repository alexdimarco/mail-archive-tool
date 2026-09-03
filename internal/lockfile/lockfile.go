// Package lockfile holds an exclusive advisory lock on an archive directory for
// the lifetime of a run, so a scheduled run and a manual run on the same -out
// can never interleave their manifest, index, and file writes (R5). The lock is
// an OS-level file lock (flock on Unix, LockFileEx on Windows): it is released
// when the holding process ends, however it ends, so a crash can never leave a
// stale lock behind. The file's content names the holder for the refusal
// message and for `status`.
package lockfile

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Name is the lock file's name inside the archive directory.
const Name = ".mailarchive.lock"

// ErrHeld is returned (wrapped) when another process holds the lock.
var ErrHeld = errors.New("held by another run")

// Lock is a held lock.
type Lock struct {
	f    *os.File
	path string
}

// Acquire takes the exclusive lock at path without waiting. On success the
// file records "pid=… started=… host=…". When another process holds it the
// error wraps ErrHeld and names the path and, when readable, the holder.
func Acquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	if err := tryLock(f); err != nil {
		f.Close()
		holder := readHolder(path)
		if holder != "" {
			holder = " (" + holder + ")"
		}
		return nil, fmt.Errorf("archive is in use by another mailarchive run%s: %s: %w", holder, path, ErrHeld)
	}
	host, _ := os.Hostname()
	info := fmt.Sprintf("pid=%d started=%s host=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339), host)
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(info), 0)
	return &Lock{f: f, path: path}, nil
}

// Release unlocks and closes the lock file. The file itself is left in place
// (removing it would race a concurrent Acquire); its content is stale once no
// process holds it, which is why holders are always re-verified by locking,
// never by reading.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := unlock(l.f)
	l.f.Close()
	l.f = nil
	return err
}

// Holder returns the pid/started/host line written by the current holder, or
// "" when the file is absent or unreadable.
func Holder(path string) string { return readHolder(path) }

func readHolder(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
