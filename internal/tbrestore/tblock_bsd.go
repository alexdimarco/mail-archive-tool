//go:build !windows && !linux

package tbrestore

import "golang.org/x/sys/unix"

// On macOS and the other Unixes the classic per-process kind, the only one there
// (design §5.2 note item 2; Mac lab of 2026-11-06), held under the sole-opener rule.
const (
	getLockCmd = unix.F_GETLK
	setLockCmd = unix.F_SETLK
)
