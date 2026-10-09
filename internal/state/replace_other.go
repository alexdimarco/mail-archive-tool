//go:build !windows

package state

// renameInUse classifies a rename error replaceFile waits out: none here.
// POSIX rename(2) replaces a file other processes hold open, and its failures
// (EACCES on an unwritable folder, EXDEV, EISDIR, ...) do not clear by
// waiting, so a save is attempted once. A var so a test can substitute it.
var renameInUse = func(error) bool { return false }
