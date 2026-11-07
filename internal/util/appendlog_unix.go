//go:build !windows

package util

import "syscall"

// appendNoFollow opens a journal without following a link at its name.
const appendNoFollow = syscall.O_NOFOLLOW
