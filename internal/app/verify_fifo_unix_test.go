//go:build unix

package app

import (
	"syscall"
	"testing"
)

// makeFIFO creates a named pipe at path so a test can prove verify classifies a
// non-regular file without opening it (opening a FIFO would block forever).
// Returns true when a FIFO was created (always, on unix).
func makeFIFO(t *testing.T, path string) bool {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Fatalf("mkfifo %s: %v", path, err)
	}
	return true
}
