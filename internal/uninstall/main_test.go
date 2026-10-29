package uninstall

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/progcache"
)

// TestMain points MailArchive's scratch folder at a throwaway folder for the whole
// package, so no uninstall test can ever read or remove the real one of the machine
// running the tests. The home, settings, cache and state folders are throwaway too
// (MA-894); a test that needs its own sets them again with t.Setenv.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "uninstall-test-scratch-")
	if err != nil {
		panic(err)
	}
	scratchRoot = func() (string, error) { return filepath.Join(dir, "scratch"), nil }
	// Before the home moves, so the real program cache is the one guarded.
	cacheDone := progcache.UseTestRoot(filepath.Join(dir, "program-cache"))
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		panic(err)
	}
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA"} {
		os.Setenv(v, home)
	}
	code := m.Run()
	if err := cacheDone(); err != nil {
		println("TestMain:", err.Error())
		code = 1
	}
	os.RemoveAll(dir)
	os.Exit(code)
}
