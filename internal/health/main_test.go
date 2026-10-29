package health

import (
	"os"
	"testing"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// TestMain points this computer's record of encrypted archives at a throwaway
// folder, so status's tests never read the developer's real record. The home,
// settings, cache and state folders are throwaway too, so Gather never reads the
// developer's Desktop instance file or last failure record (MA-894); a test that
// needs its own sets them again with t.Setenv. Its tests make keys at the cheap test
// cost (MA-776).
func TestMain(m *testing.M) {
	vault.UseTestCost()
	dir, err := os.MkdirTemp("", "mailarchive-health-record-")
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
