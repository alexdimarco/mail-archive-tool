//go:build linux

package procs

import (
	"os"
	"strconv"
	"strings"
)

// Program is the full path of the program process pid runs, read from the kernel
// (/proc/<pid>/exe), so a caller can tell what a recorded pid is now before it acts on it.
// A program file replaced since it started (an upgrade) is still named by its path.
func Program(pid int) (string, error) {
	p, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(p, " (deleted)"), nil
}
