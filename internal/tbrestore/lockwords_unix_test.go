//go:build !windows

package tbrestore

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// This platform's errors for the lock words' table (MA-1095 lab BUG-2): a link at
// .parentlock is held (O_NOFOLLOW's ELOOP); a take refused with EACCES is a conflicting
// lock, not a permission (POSIX F_SETLK); ENOLCK (a file system with no locks) is other.
func heldOpenErr() error   { return unix.ELOOP }
func heldTakeErr() error   { return unix.EACCES }
func permErr() error       { return unix.EACCES }
func readOnlyErrno() error { return unix.EROFS }
func otherTakeErr() error  { return unix.ENOLCK }

// unmakeableLocks are this platform's real profiles whose lock file cannot be made or
// opened. Root may open anything, so the permission legs need an ordinary account.
func unmakeableLocks(t *testing.T) []lockLeg {
	lockFile := func(p string) []string { return []string{filepath.Join(p, ParentLockName)} }
	// A folder at the lock name is worded at the plan's check as at Start (settled
	// 2026-11-07 [ADV-L1, ADV-L2]: never "closed" at Check and refused at Start).
	legs := []lockLeg{{
		what: "a folder at the lock name", code: "W-LOCKFAIL", names: func(p string) []string { return []string{filepath.Join(p, ParentLockName), "not an ordinary file"} },
		set: func(t *testing.T, p string) func() {
			if err := os.MkdirAll(filepath.Join(p, ParentLockName), 0o700); err != nil {
				t.Fatal(err)
			}
			return func() {}
		},
	}}
	if os.Geteuid() == 0 {
		return legs
	}
	chmod := func(t *testing.T, p string, m os.FileMode) {
		t.Helper()
		if err := os.Chmod(p, m); err != nil {
			t.Fatal(err)
		}
	}
	return append(legs,
		lockLeg{what: "a profile folder this account may not change", code: "W-LOCKPERM", atStartOnly: true, names: lockFile,
			set: func(t *testing.T, p string) func() {
				chmod(t, p, 0o500)
				return func() { chmod(t, p, 0o700) }
			}},
		lockLeg{what: "a lock file this account may only read", code: "W-LOCKPERM", atStartOnly: true, names: lockFile,
			set: func(t *testing.T, p string) func() {
				writeFile(t, filepath.Join(p, ParentLockName), "")
				chmod(t, filepath.Join(p, ParentLockName), 0o400)
				return func() { chmod(t, filepath.Join(p, ParentLockName), 0o600) }
			}},
		lockLeg{what: "a lock file this account may not open at all", code: "W-LOCKPERM", names: lockFile,
			set: func(t *testing.T, p string) func() {
				writeFile(t, filepath.Join(p, ParentLockName), "")
				chmod(t, filepath.Join(p, ParentLockName), 0o000)
				return func() { chmod(t, filepath.Join(p, ParentLockName), 0o600) }
			}},
	)
}
