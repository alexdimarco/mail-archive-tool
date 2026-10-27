//go:build linux

package app

import (
	"os"
	"strings"
)

// removableDrive reads the mount holding abs from /proc/self/mountinfo
// (removableMountinfo). A mountinfo that cannot be read is not called removable.
func removableDrive(abs string) bool {
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	return removableMountinfo(strings.Split(string(b), "\n"), abs)
}
