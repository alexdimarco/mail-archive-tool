//go:build !windows

package util

import "os"

// createSecretTemp is os.CreateTemp: Unix has no integrity levels, and the 0600 mode the
// writer sets keeps every other account out.
func createSecretTemp(dir, pattern string) (*os.File, error) { return createTemp(dir, pattern) }
