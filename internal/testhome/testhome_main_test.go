package testhome

import (
	"os"
	"testing"
)

// TestMain runs this package's own tests in a throw-away home, through its own Run, as every
// package whose tests can reach the settings folder does (MA-1333; assureblock's meta-test). A test
// that points the folders elsewhere sets them again with t.Setenv.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "mailarchive-testhome-home-")
	if err != nil {
		panic(err)
	}
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA"} {
		os.Setenv(v, root)
	}
	code := Run(root, m)
	os.RemoveAll(root)
	os.Exit(code)
}
