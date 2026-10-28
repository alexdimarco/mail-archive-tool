//go:build !darwin && !linux

package util

import "os"

// changeTimeSame is not compared here: Windows has no change time Go exposes, and its
// file index is not reused at once; other systems are not release targets.
func changeTimeSame(a, b os.FileInfo) bool { return true }
