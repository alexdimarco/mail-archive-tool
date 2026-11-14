package presync

import (
	"os"
	"testing"

	"mail-archive-tool/internal/testhome"
)

// childMain, when a platform's tests set it, runs this test binary as a child helper instead of the
// tests (Windows: the stand-in program a launch is checked against); ok false runs the tests.
var childMain func() (code int, ok bool)

// TestMain points the home, settings, cache and state folders at a throw-away folder for the whole
// package and runs the tests through testhome.Run, so none reaches this computer's real per-user
// folders (MailArchive's settings folder, where the restore lock lives; a Thunderbird profile)
// even to read them (MA-1333; assureblock's meta-test). A test that needs its own sets them again
// with t.Setenv; a child helper inherits the throw-away folders.
func TestMain(m *testing.M) {
	if childMain != nil {
		if code, ok := childMain(); ok {
			os.Exit(code)
		}
	}
	root, err := os.MkdirTemp("", "mailarchive-presync-home-")
	if err != nil {
		panic(err)
	}
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA"} {
		os.Setenv(v, root)
	}
	code := testhome.Run(root, m)
	os.RemoveAll(root)
	os.Exit(code)
}
