//go:build !windows

package util

// IsHydratedCloudFile reports a downloaded cloud file that Go's mode bits call
// irregular; only Windows has them.
func IsHydratedCloudFile(string) bool { return false }
