package wizard

import (
	"os"
	"testing"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// TestMain installs a no-op backup-alert sender for the whole package, so a test
// that exercises a scheduled run's failure path (RunHeadless -> FireForRunDefault)
// can NEVER send a real email through whatever alert config happens to be on the
// machine running the tests. Tests that assert on the alert install their own
// recording sender via SetSenderForTest and restore to this no-op. Its tests
// make keys at the cheap test cost (MA-776).
func TestMain(m *testing.M) {
	vault.UseTestCost()
	backupalert.SetSenderForTest(func(alert.Config, string, alert.Event) error { return nil })
	// This computer's record of encrypted archives, which every writer reads, is a
	// throwaway one: never the developer's.
	dir, err := os.MkdirTemp("", "mailarchive-wizard-record-")
	if err != nil {
		panic(err)
	}
	restore := encrecord.SetDirForTest(dir)
	// The home, settings and cache folders are throwaway for the whole package, so a
	// test that writes a job, a log or a scratch file never reaches the developer's
	// (the P3-3 plan, 3a). A test that needs its own sets them again with t.Setenv.
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "AppData", "LOCALAPPDATA"} {
		os.Setenv(v, dir)
	}
	code := m.Run()
	restore()
	os.RemoveAll(dir)
	os.Exit(code)
}
