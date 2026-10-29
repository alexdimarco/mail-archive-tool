package archindex

import (
	"os"
	"testing"

	"mail-archive-tool/internal/encrecord"
)

// TestMain points this computer's record of encrypted archives at a throwaway
// folder, so the archive list's tests never read the developer's real record. The
// home, settings, cache and state folders are throwaway too (MA-894); a test that
// needs its own sets them again with t.Setenv.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mailarchive-archindex-record-")
	if err != nil {
		panic(err)
	}
	restore := encrecord.SetDirForTest(dir)
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA"} {
		os.Setenv(v, dir)
	}
	code := m.Run()
	restore()
	os.RemoveAll(dir)
	os.Exit(code)
}
