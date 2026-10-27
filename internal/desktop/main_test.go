package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// TestMain points the per-user config dir (job.Dir, via os.UserConfigDir) at a
// throwaway folder for the whole package, so a test that installs a schedule —
// which writes its job file there — never leaves files in the real MailArchive
// settings folder of the machine running the tests. Its tests make keys at the
// cheap test cost (MA-776). The cache folder (scratch indexes), this computer's
// record of encrypted archives and the alert sender are throwaway too, so a test
// that unlocks or backs up an encrypted archive touches none of the developer's
// (the P3-3 plan, 3a).
func TestMain(m *testing.M) {
	vault.UseTestCost()
	backupalert.SetSenderForTest(func(alert.Config, string, alert.Event) error { return nil })
	home, err := os.MkdirTemp("", "desktop-test-home-")
	if err != nil {
		panic(err)
	}
	for _, v := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "AppData", "LOCALAPPDATA"} {
		os.Setenv(v, home)
	}
	restore := encrecord.SetDirForTest(filepath.Join(home, "record"))
	code := m.Run()
	restore()
	os.RemoveAll(home)
	os.Exit(code)
}
