//go:build !windows

package winsec

// RestrictToOwner does nothing off Windows: a file created 0600 is already the
// owner's alone.
func RestrictToOwner(path string) error { return nil }
