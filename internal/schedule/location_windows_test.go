//go:build windows

package schedule

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// covers: MA-649, R33, S59
// L-W15 — the live Windows DACL read, verified on the Parallels Windows box: a
// directory another account can write is flagged, an owner/SYSTEM/Administrators-
// only one is not, and ProgramForSchedule refuses from the writable one with
// ErrUnsafeLocation. Uses SID literals with icacls so it is locale-independent:
// S-1-5-11 Authenticated Users, S-1-5-18 SYSTEM, S-1-5-32-544 Administrators.
func TestLW15_WindowsDACLLiveRead(t *testing.T) {
	icacls := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("icacls", args...).CombinedOutput(); err != nil {
			t.Fatalf("icacls %v failed: %v\n%s", args, err, out)
		}
	}

	// Authenticated Users: Modify → writable by others.
	shared := t.TempDir()
	icacls(shared, "/grant", "*S-1-5-11:(OI)(CI)M")
	if !locationWritableByOthersOS(shared) {
		t.Errorf("a dir granting Authenticated Users Modify must be writable-by-others: %s", shared)
	}

	// SYSTEM + Administrators only (inheritance removed) → safe. The test runs as
	// SYSTEM under prlctl, so the dir's owner is a system principal and both ACEs
	// are safe SIDs; granting by a human USERNAME is skipped (it is a machine
	// account here and would not map).
	safe := filepath.Join(t.TempDir(), "safe")
	if err := os.Mkdir(safe, 0o700); err != nil {
		t.Fatal(err)
	}
	icacls(safe, "/inheritance:r")
	icacls(safe, "/grant:r", "*S-1-5-18:(OI)(CI)F")
	icacls(safe, "/grant:r", "*S-1-5-32-544:(OI)(CI)F")
	if locationWritableByOthersOS(safe) {
		t.Errorf("a SYSTEM/Administrators-only dir must be safe: %s", safe)
	}

	// The guard flows through ProgramForSchedule: a program under the shared dir is
	// refused with ErrUnsafeLocation, one under the safe dir is not.
	origSelf := SelfResolver
	t.Cleanup(func() { SelfResolver = origSelf })
	SelfResolver = func() (string, error) { return filepath.Join(safe, "mailarchivew.exe"), nil }
	if _, _, err := ProgramForSchedule(false); err == ErrUnsafeLocation {
		t.Errorf("a program in an owner-only dir must not be refused for location")
	}
	SelfResolver = func() (string, error) { return filepath.Join(shared, "mailarchivew.exe"), nil }
	if _, _, err := ProgramForSchedule(false); err != ErrUnsafeLocation {
		t.Errorf("a program in an Authenticated-Users-writable dir must be refused with ErrUnsafeLocation, got %v", err)
	}
}
