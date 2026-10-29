package schedule

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// testHome is the throwaway home the whole package runs under.
var testHome string

// TestMain points this computer's record of encrypted archives at a throwaway
// folder, so the install guard's tests never read the developer's real record.
// The home, settings, cache and state folders are throwaway too, for every OS, so a
// test that installs a schedule never writes its job file into the developer's
// (on Windows %APPDATA%\mailarchive; MA-894). A test that needs its own sets them
// again with t.Setenv. Its tests make keys at the cheap test cost (MA-776).
func TestMain(m *testing.M) {
	vault.UseTestCost()
	dir, err := os.MkdirTemp("", "mailarchive-schedule-record-")
	if err != nil {
		panic(err)
	}
	restore := encrecord.SetDirForTest(dir)
	testHome = filepath.Join(dir, "home")
	if err := os.MkdirAll(testHome, 0o700); err != nil {
		panic(err)
	}
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA"} {
		os.Setenv(v, testHome)
	}
	code := m.Run()
	restore()
	os.RemoveAll(dir)
	os.Exit(code)
}
