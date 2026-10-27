//go:build !darwin && !linux && !windows

package app

// removableDrive cannot tell on this system; nothing is called removable.
func removableDrive(string) bool { return false }
