//go:build unix

package server

import "syscall"

// openFlags is what /files/ opens a file with: read-only, and never waiting for
// a writer, so a FIFO swapped in after the checks cannot block the handler; the
// opened entry's kind is checked right after (AC2).
const openFlags = syscall.O_RDONLY | syscall.O_NONBLOCK
