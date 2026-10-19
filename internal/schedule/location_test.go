//go:build !windows

package schedule

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-648, R33, S59
// Q20 (§5.3, owner ruling 13.44 — refuse, do NOT accept as a residual): a
// schedule is refused when the program's install directory, or any ancestor, is
// world-writable without the sticky bit, because another account could replace
// the binary and the next unattended run would execute their code as the owner.
// ProgramForSchedule is the one chokepoint, so every scheduling caller refuses
// with ErrUnsafeLocation and its relocation remedy. Positive twin first: an
// owner-only directory schedules. A sticky world-writable root (/tmp) does NOT
// trip it. Group-writable is a stated residual; the Windows DACL mechanism is
// MA-649 (lab L-W15), dormant here.
func TestMA648_RefuseWorldWritableInstallLocation(t *testing.T) {
	// The world-writable mode walk, directly.
	safe := t.TempDir()
	if err := os.Chmod(safe, 0o700); err != nil {
		t.Fatal(err)
	}
	if locationWritableByOthersOS(safe) {
		t.Errorf("an owner-only directory (%s) must be safe", safe)
	}

	ww := t.TempDir()
	if err := os.Chmod(ww, 0o777); err != nil {
		t.Fatal(err)
	}
	if !locationWritableByOthersOS(ww) {
		t.Errorf("a world-writable directory (%s) must be unsafe", ww)
	}

	// A world-writable ancestor is unsafe too (rename-and-substitute the child).
	child := filepath.Join(ww, "bin")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if !locationWritableByOthersOS(child) {
		t.Errorf("an owner-only dir under a world-writable parent (%s) must be unsafe", child)
	}

	// A sticky world-writable directory (mode 1777, like /tmp) is NOT flagged: the
	// sticky bit stops others deleting or replacing files they do not own.
	sticky := t.TempDir()
	if err := os.Chmod(sticky, os.ModeSticky|0o777); err != nil {
		t.Fatal(err)
	}
	if locationWritableByOthersOS(sticky) {
		t.Errorf("a sticky world-writable directory (%s) must be safe (sticky protects existing files)", sticky)
	}

	// ProgramForSchedule refuses from an unsafe location and schedules from a safe
	// one — the one chokepoint for every caller.
	origSelf := SelfResolver
	t.Cleanup(func() { SelfResolver = origSelf })

	SelfResolver = func() (string, error) { return filepath.Join(safe, "mailarchive"), nil }
	if _, _, err := ProgramForSchedule(false); err != nil {
		t.Errorf("a program in an owner-only folder must schedule, got %v", err)
	}
	SelfResolver = func() (string, error) { return filepath.Join(ww, "mailarchive"), nil }
	if _, _, err := ProgramForSchedule(false); !errors.Is(err, ErrUnsafeLocation) {
		t.Errorf("a program in a world-writable folder must be refused with ErrUnsafeLocation, got %v", err)
	}
}
