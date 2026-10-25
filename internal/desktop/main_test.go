package desktop

import (
	"os"
	"testing"
)

// TestMain points the per-user config dir (job.Dir, via os.UserConfigDir) at a
// throwaway folder for the whole package, so a test that installs a schedule —
// which writes its job file there — never leaves files in the real MailArchive
// settings folder of the machine running the tests.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "desktop-test-home-")
	if err != nil {
		panic(err)
	}
	for _, v := range []string{"HOME", "XDG_CONFIG_HOME", "AppData", "LOCALAPPDATA"} {
		os.Setenv(v, home)
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
