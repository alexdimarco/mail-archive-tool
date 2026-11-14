package archdelete

import (
	"os"
	"testing"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/testhome"
)

// TestMain points the home, settings, cache and state folders, and this
// computer's record of encrypted archives, at a throwaway folder (MA-894), so no
// plan in this package reads the real job files, settings or record of the
// machine running the tests.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mailarchive-archdelete-")
	if err != nil {
		panic(err)
	}
	restore := encrecord.SetDirForTest(dir + "/record")
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA"} {
		os.Setenv(v, dir+"/home")
	}
	if err := os.MkdirAll(dir+"/home", 0o700); err != nil {
		panic(err)
	}
	// Checked before and after the tests: the settings folder resolves inside the throw-away home.
	code := testhome.Run(dir+"/home", m)
	restore()
	os.RemoveAll(dir)
	os.Exit(code)
}
