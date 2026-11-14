//go:build darwin

package procs

import (
	"bytes"
	"errors"

	"golang.org/x/sys/unix"
)

// Program is the full path of the program process pid runs, read from the kernel
// (sysctl kern.procargs2: its argument count, then the path it was started from), so a
// caller can tell what a recorded pid is now before it acts on it.
func Program(pid int) (string, error) {
	b, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", err
	}
	if len(b) < 5 {
		return "", errors.New("the process's arguments are too short to name its program")
	}
	path := b[4:] // after the argument count

	if i := bytes.IndexByte(path, 0); i >= 0 {
		path = path[:i]
	}
	if len(path) == 0 {
		return "", errors.New("the process names no program")
	}
	return string(path), nil
}
