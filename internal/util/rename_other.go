//go:build !windows

package util

// renameInUse classifies a rename error the atomic writer waits out: none here
// — a POSIX rename replaces a file others hold open.
var renameInUse = func(error) bool { return false }
