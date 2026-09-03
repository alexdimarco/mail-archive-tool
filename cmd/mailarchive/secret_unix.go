//go:build !windows

package main

import "syscall"

// secretOpenFlags: never follow a symlink, never block on a FIFO.
const secretOpenFlags = syscall.O_NOFOLLOW | syscall.O_NONBLOCK
