//go:build !windows

package app

import "syscall"

// openNoBlock makes open(2) of a named pipe return at once instead of waiting for a
// writer; a regular file ignores it.
const openNoBlock = syscall.O_NONBLOCK
