package progcache

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the cache's default folder at a throwaway one for the whole package
// and fails if the real one was touched (program design R22). Then the home, settings,
// cache and state folders are throwaway too (MA-894); a test that needs its own sets
// them again with t.Setenv.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "mailarchive-progcache-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain:", err)
		os.Exit(1)
	}
	// Before the home moves, so the real cache is the one guarded.
	done := UseTestRoot(filepath.Join(tmp, "mailarchive", "program-cache"))
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "TestMain:", err)
		os.Exit(1)
	}
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA"} {
		os.Setenv(v, home)
	}
	code := m.Run()
	if err := done(); err != nil {
		fmt.Fprintln(os.Stderr, "TestMain:", err)
		code = 1
	}
	os.RemoveAll(tmp)
	os.Exit(code)
}
