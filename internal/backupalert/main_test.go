package backupalert

import (
	"os"
	"testing"

	"mail-archive-tool/internal/alert"
)

// TestMain installs a no-op mail sender for the whole package, so no test can send a
// real email through whatever alert is set up on the machine running the tests; a test
// that asserts on the alert installs its own recording sender. The home, settings,
// cache and state folders are throwaway too, so no streak or alert setting of the
// developer's is read or written (MA-894); a test that needs its own sets them again
// with t.Setenv.
func TestMain(m *testing.M) {
	SetSenderForTest(func(alert.Config, string, alert.Event) error { return nil })
	dir, err := os.MkdirTemp("", "mailarchive-backupalert-")
	if err != nil {
		panic(err)
	}
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA"} {
		os.Setenv(v, dir)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
