//go:build !windows

package util

// resolveLinks is not needed off Windows: filepath.EvalSymlinks already follows
// every link there.
func resolveLinks(abs string) (string, bool) { return "", false }
