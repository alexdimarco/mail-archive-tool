//go:build !windows

package winsec

// RestrictToOwner does nothing off Windows: a file created 0600 is already the
// owner's alone.
func RestrictToOwner(path string) error { return nil }

// RestrictFolderToOwner does nothing off Windows, as RestrictToOwner.
func RestrictFolderToOwner(path string) error { return nil }
