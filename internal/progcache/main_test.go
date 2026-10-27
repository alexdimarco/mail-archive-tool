package progcache

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the cache's default folder at a throwaway one for the whole package
// and fails if the real one was touched (program design R22).
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "mailarchive-progcache-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain:", err)
		os.Exit(1)
	}
	done := UseTestRoot(filepath.Join(tmp, "mailarchive", "program-cache"))
	code := m.Run()
	if err := done(); err != nil {
		fmt.Fprintln(os.Stderr, "TestMain:", err)
		code = 1
	}
	os.RemoveAll(tmp)
	os.Exit(code)
}
