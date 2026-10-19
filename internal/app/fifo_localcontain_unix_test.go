//go:build !windows

package app

import "syscall"

// mkfifo makes a pipe (FIFO) at path: what a hostile store plants where a mail file
// should be, and what a plain open would wait on.
func mkfifo(path string) error { return syscall.Mkfifo(path, 0o644) }
