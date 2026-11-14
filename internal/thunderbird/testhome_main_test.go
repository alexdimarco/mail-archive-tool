package thunderbird

import (
	"os"
	"testing"

	"mail-archive-tool/internal/testhome"
)

// TestMain points the home, settings, cache and state folders at a throw-away folder for the whole
// package and runs the tests through testhome.Run, so none reaches this computer's real per-user
// folders (MailArchive's settings folder, a Thunderbird profile) even to read them (MA-1333;
// assureblock's meta-test). A test that needs its own sets them again with t.Setenv.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "mailarchive-thunderbird-home-")
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
