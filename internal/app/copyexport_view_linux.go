//go:build linux

package app

import (
	"os"
	"strings"
)

// classifyView classifies how abs is reached from /proc/self/mountinfo: the mount that
// holds the path, its filesystem type and its mount source (design §4.7, RR-2b
// re-verification). A mountinfo that cannot be read leaves the view unknown, which is
// stacking: identity is then never trusted to decide.
func classifyView(abs string) ceView {
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return ceView{Kind: viewStacking}
	}
	return classifyMountinfo(strings.Split(string(b), "\n"), abs)
}
