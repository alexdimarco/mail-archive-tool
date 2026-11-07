//go:build linux

package tbrestore

import "golang.org/x/sys/unix"

// On Linux the per-handle ("open file description") lock kind (design §5.2 note
// item 2; Linux lab of 2026-11-06: it conflicts with Thunderbird's both ways and
// survives another descriptor's close).
const (
	getLockCmd = unix.F_OFD_GETLK
	setLockCmd = unix.F_OFD_SETLK
)
