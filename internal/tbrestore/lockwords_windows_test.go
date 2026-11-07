//go:build windows

package tbrestore

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// This platform's errors for the lock words' table (MA-1095 lab BUG-2): parent.lock open
// with no sharing (Thunderbird's share 0) is held; a read-only share answers "access
// denied"; a write-protected medium ERROR_WRITE_PROTECT.
func heldOpenErr() error   { return windows.ERROR_SHARING_VIOLATION }
func heldTakeErr() error   { return windows.ERROR_LOCK_VIOLATION }
func permErr() error       { return windows.ERROR_ACCESS_DENIED }
func readOnlyErrno() error { return windows.ERROR_WRITE_PROTECT }
func otherTakeErr() error  { return windows.ERROR_NOT_SUPPORTED }

// unmakeableLocks: a parent.lock with the read-only attribute cannot be opened for
// writing ("access denied"), as on the lab's read-only share; the plan's probe sees that
// too, so both the plan and Start word it.
func unmakeableLocks(t *testing.T) []lockLeg {
	return []lockLeg{{
		what: "a read-only parent.lock", code: "W-LOCKPERM",
		names: func(p string) []string { return []string{filepath.Join(p, ParentLockName), "read-only network share"} },
		set: func(t *testing.T, p string) func() {
			f := filepath.Join(p, ParentLockName)
			writeFile(t, f, "")
			if err := os.Chmod(f, 0o444); err != nil {
				t.Fatal(err)
			}
			return func() { os.Chmod(f, 0o666) }
		},
	}}
}
