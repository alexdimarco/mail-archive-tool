//go:build !windows

package util

import "syscall"

// secureOpenFlags: never follow a symlink, never block on a FIFO.
const secureOpenFlags = syscall.O_NOFOLLOW | syscall.O_NONBLOCK
