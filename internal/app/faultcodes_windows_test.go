//go:build windows

package app

import "syscall"

// Windows twin of faultcodes_unix_test.go: the real Windows volume-error codes the
// product's destinationFatal / FatalKind match (112/39 = full, 19 = read-only), so
// a fault-injection test simulates a fault Windows actually produces.
var (
	errFull     syscall.Errno = 112 // ERROR_DISK_FULL        -> FatalFull
	errQuota    syscall.Errno = 39  // ERROR_HANDLE_DISK_FULL -> FatalFull
	errReadOnly syscall.Errno = 19  // ERROR_WRITE_PROTECT    -> FatalReadOnly
)
