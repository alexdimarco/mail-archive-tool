package desktop

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/progcache"
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
	// Before HOME moves, so the real program cache is the one guarded (R22).
	cacheDone := progcache.UseTestRoot(filepath.Join(home, "program-cache"))
	// A small file stands in for this program, so a new archive's self-copy is not the
	// whole test binary. On Windows it is named mailarchive.exe, the console program a
	// self-copy is made from there.
	stand := filepath.Join(home, "mailarchive-test-program")
	if runtime.GOOS == "windows" {
		stand = filepath.Join(home, "mailarchive.exe")
	}
	os.WriteFile(stand, []byte("#!/bin/sh\necho mailarchive test program\n"), 0o755)
	app.UseTestProgram(stand)
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "AppData", "LOCALAPPDATA"} {
		os.Setenv(v, home)
	}
	restore := encrecord.SetDirForTest(filepath.Join(home, "record"))
	code := m.Run()
	restore()
	if err := cacheDone(); err != nil {
		println("TestMain:", err.Error())
		code = 1
	}
	os.RemoveAll(home)
	os.Exit(code)
}
