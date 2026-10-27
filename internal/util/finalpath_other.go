//go:build !windows

package util

// finalPath is p: off Windows, filepath.EvalSymlinks already follows every link.
func finalPath(p string) (string, bool) { return p, true }
