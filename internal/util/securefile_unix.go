//go:build !windows

package util

import "syscall"

// secureOpenFlags: never follow a symlink, never block on a FIFO.
const secureOpenFlags = syscall.O_NOFOLLOW | syscall.O_NONBLOCK

// rootOpenFlags: what an os.Root open adds. The root's own open never follows a link
// out of it, so only the non-blocking flag is left to ask for.
const rootOpenFlags = syscall.O_NONBLOCK
