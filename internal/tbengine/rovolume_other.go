//go:build !darwin && !linux && !windows

package tbengine

// isReadOnlyVolume is not asked on the other systems: the lock is taken as usual.
func isReadOnlyVolume(string) bool { return false }

// lockDirWritable: Start's own take answers, in words (W-ARCHLOCK).
func lockDirWritable(string) error { return nil }
