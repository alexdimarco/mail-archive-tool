//go:build !windows

package util

// resolveLinks is not needed off Windows: filepath.EvalSymlinks already follows
// every link there.
func resolveLinks(abs string) (string, bool) { return "", false }

// ThroughLink is Windows' junction question; off Windows it is not asked (false):
// filepath.EvalSymlinks follows every link there, and the profile's own checks judge
// the folder it leads to.
func ThroughLink(abs string) bool { return false }
