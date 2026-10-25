package uninstall

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points MailArchive's scratch folder at a throwaway folder for the whole
// package, so no uninstall test can ever read or remove the real one of the machine
// running the tests.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "uninstall-test-scratch-")
	if err != nil {
		panic(err)
	}
	scratchRoot = func() (string, error) { return filepath.Join(dir, "scratch"), nil }
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
