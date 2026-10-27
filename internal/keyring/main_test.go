package keyring

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// TestMain runs the package at the cheap key cost (MA-776) with this computer's
// record, MailArchive's settings folder and the home folder all in a throwaway
// folder, so no test reads or writes the developer's own.
func TestMain(m *testing.M) {
	vault.UseTestCost()
	root, err := os.MkdirTemp("", "mailarchive-keyring-")
	if err != nil {
		panic(err)
	}
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		panic(err)
	}
	for _, v := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "AppData", "LOCALAPPDATA"} {
		os.Setenv(v, home)
	}
	// A small file stands in for this program, so an archive's copy of MailArchive is
	// not the whole test binary. On Windows it is named mailarchive.exe, the console
	// program a self-copy is made from there.
	stand := filepath.Join(root, "mailarchive-test-program")
	if runtime.GOOS == "windows" {
		stand = filepath.Join(root, "mailarchive.exe")
	}
	if err := os.WriteFile(stand, []byte("#!/bin/sh\necho mailarchive test program\n"), 0o755); err != nil {
		panic(err)
	}
	app.UseTestProgram(stand)
	restore := encrecord.SetDirForTest(filepath.Join(root, "record"))
	code := m.Run()
	restore()
	os.RemoveAll(root)
	os.Exit(code)
}
