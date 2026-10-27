package uninstall

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/progcache"
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
	cacheDone := progcache.UseTestRoot(filepath.Join(dir, "program-cache"))
	code := m.Run()
	if err := cacheDone(); err != nil {
		println("TestMain:", err.Error())
		code = 1
	}
	os.RemoveAll(dir)
	os.Exit(code)
}
