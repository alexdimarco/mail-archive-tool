package lockfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// covers: MA-85, R5, R4, S25
// The lock never writes through a planted symlink (a link at the lock path is
// refused, the target untouched); a held lock notices when its file is removed
// or replaced under it (StillHeld) so the run can stop instead of racing a new
// holder; and the holder line echoed into refusals has its control characters
// stripped.
func TestLockHardening(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(victim, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, Name)
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if l, err := Acquire(link); err == nil {
		l.Release()
		t.Fatal("acquired through a symlink")
	} else if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("symlink refusal must say why: %v", err)
	}
	if data, _ := os.ReadFile(victim); string(data) != "precious" {
		t.Errorf("symlink target was modified: %q", data)
	}
	os.Remove(link)

	// Removed / replaced under the holder.
	l, err := Acquire(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.StillHeld(); err != nil {
		t.Fatalf("fresh lock reported lost: %v", err)
	}
	os.Remove(link)
	if err := l.StillHeld(); err == nil || !strings.Contains(err.Error(), "removed") {
		t.Errorf("removed lock not detected: %v", err)
	}
	os.WriteFile(link, []byte("pid=1 started=x host=y"), 0o644)
	if err := l.StillHeld(); err == nil || !strings.Contains(err.Error(), "replaced") {
		t.Errorf("replaced lock not detected: %v", err)
	}
	l.Release()

	// Hostile holder content is sanitized before it reaches a terminal.
	hostile := filepath.Join(dir, "hostile.lock")
	os.WriteFile(hostile, []byte("pid=1\x1b[2J\x07 started=now\r\nhost=evil"), 0o644)
	if h := Holder(hostile); strings.ContainsAny(h, "\x1b\x07\r\n") {
		t.Errorf("holder line carries control characters: %q", h)
	}
}

// covers: MA-85, R5, S25
// The lock file is created private like every other archive file (its holder
// line names a pid and a host).
func TestLockFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	path := filepath.Join(t.TempDir(), Name)
	l, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Errorf("lock file mode = %o, want 600", mode)
	}
}
