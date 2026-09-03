//go:build !unix

package app

import "testing"

// makeFIFO is a no-op on platforms without mkfifo (Windows); the FIFO sub-case
// of the containment test is skipped there.
func makeFIFO(t *testing.T, path string) bool {
	t.Helper()
	return false
}
