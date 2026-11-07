//go:build !linux && !darwin && !windows

package tbrestore

// thunderbirdProcessCount: no process probe on this platform, so the list is not
// known and Thunderbird counts as running (a mistake only refuses).
func thunderbirdProcessCount() (int, bool) { return 0, false }
