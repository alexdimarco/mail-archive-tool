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

// Acquire takes the exclusive lock at path without waiting, naming no verb (the
// holder line carries "pid=… started=… host=…"). See AcquireAs.
func Acquire(path string) (*Lock, error) { return AcquireAs(path, "") }

// AcquireAs is Acquire naming the verb that holds the lock, so a run refused
// while another holds it reads "held by mailarchive <verb> …" — the export,
// Graph, reindex or verify that is running is self-explaining (FC5). On success
// the file records "pid=… started=… host=… verb=<verb>" (verb omitted when
// empty). When another process holds it the error wraps ErrHeld and names the
// path and, when readable, the holder.
func AcquireAs(path, verb string) (*Lock, error) {
	// Never follow a symlink: a lock file that points elsewhere would be
	// truncated when the holder line is written (an insider's planted link).
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("lock %s is not a regular file (a symlink or special file was planted there); remove it", path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|noFollow, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	if err := tryLock(f); err != nil {
		f.Close()
		holder := readHolder(path)
		if holder != "" {
			holder = " (held by mailarchive: " + holder + ")"
		}
		return nil, fmt.Errorf("archive is in use by another mailarchive run%s: %s: %w", holder, path, ErrHeld)
	}
	host, _ := os.Hostname()
	info := fmt.Sprintf("pid=%d started=%s host=%s", os.Getpid(), time.Now().UTC().Format(time.RFC3339), host)
	if verb = sanitizeVerb(verb); verb != "" {
		info += " verb=" + verb
	}
	info += "\n"
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(info), 0)
	return &Lock{f: f, path: path}, nil
}

// sanitizeVerb keeps a verb to a short, control-character-free token so the
// holder line (echoed into a terminal in a refusal) stays clean whatever a
// caller passes.
func sanitizeVerb(verb string) string {
	verb = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			return r
		default:
			return -1
		}
	}, verb)
	if len(verb) > 16 {
		verb = verb[:16]
	}
	return verb
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

// StillHeld verifies the held lock is still the file at its path: if the lock
// file was removed or replaced (another process could then hold a fresh one),
// it returns an error naming that, so the run can stop rather than race.
func (l *Lock) StillHeld() error {
	if l == nil || l.f == nil {
		return errors.New("lock not held")
	}
	held, err := l.f.Stat()
	if err != nil {
		return fmt.Errorf("lock %s: %w", l.path, err)
	}
	onDisk, err := os.Stat(l.path)
	if err != nil {
		return fmt.Errorf("lock %s was removed during the run (%v): stopping so two runs cannot overlap; run again", l.path, err)
	}
	if !os.SameFile(held, onDisk) {
		return fmt.Errorf("lock %s was replaced during the run: stopping so two runs cannot overlap; run again", l.path)
	}
	return nil
}

// Holder returns the pid/started/host line written by the current holder, or
// "" when the file is absent or unreadable. The content is untrusted (any
// writer of the archive directory can change it): control characters are
// stripped so it can be echoed into a terminal.
func Holder(path string) string { return readHolder(path) }

func readHolder(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 256)
	n, _ := f.Read(buf)
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, string(buf[:n])))
}
