package tbengine

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/tbrestore"
	"mail-archive-tool/internal/testhome"
	"mail-archive-tool/internal/vault"
)

// TestMain runs the engine's tests with MailArchive's settings folder (restores.log,
// the restore locks, job files), the home folder and this computer's record of encrypted
// archives all in a throwaway folder, at the cheap key cost (MA-776), so no test reads or
// writes the developer's own. Fake Thunderbird profiles live in temp folders only; the
// probes of the OS a test stands in for (the Thunderbird process list — the person's own
// Thunderbird may be running here —, elevation, a Microsoft Store Thunderbird, a network
// drive) answer "none" unless a test says otherwise.
//
// Run with TBE_HELPER set, the binary is a child that tries to take locks, as another
// MailArchive run or a starting Thunderbird would (MA-1127).
func TestMain(m *testing.M) {
	if mode := os.Getenv("TBE_HELPER"); mode != "" {
		fmt.Println(helper(mode, strings.Split(os.Getenv("TBE_PATHS"), string(os.PathListSeparator))))
		os.Exit(0)
	}
	vault.UseTestCost()
	root, err := os.MkdirTemp("", "mailarchive-tbengine-")
	if err != nil {
		panic(err)
	}
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		panic(err)
	}
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA"} {
		os.Setenv(v, home)
	}
	stand := filepath.Join(root, "mailarchive-test-program")
	if runtime.GOOS == "windows" {
		stand = filepath.Join(root, "mailarchive.exe")
	}
	if err := os.WriteFile(stand, []byte("#!/bin/sh\necho mailarchive test program\n"), 0o755); err != nil {
		panic(err)
	}
	app.UseTestProgram(stand)
	restoreRecord := encrecord.SetDirForTest(filepath.Join(root, "record"))
	restoreProbe := tbrestore.SetProcessProbeForTest(func() (int, bool) { return 0, true })
	elevatedProbe = func() bool { return false }
	storeWarning = func() string { return "" }
	networkWarning = func(tbrestore.LocalFolders) string { return "" }
	// This computer's profiles.ini (Windows reads %APPDATA% from the system, not the
	// environment) is never read: the engine's own look for profiles sharing a Local
	// Folders (R6-06) reads a throwaway folder unless a test names its own.
	profileBases = func() []string { return []string{filepath.Join(root, "thunderbird-profiles")} }
	// Checked before and after the tests: the settings folder resolves inside the throw-away home.
	code := testhome.Run(home, m)
	restoreProbe()
	restoreRecord()
	os.RemoveAll(root)
	os.Exit(code)
}

// helper is the child's side. "locks": for each path, take the lock there as its kind
// says (a name ending in .parentlock or parent.lock is Thunderbird's profile lock, taken
// as Thunderbird takes it; any other is a MailArchive lock, taken with lockfile.Acquire)
// and answer "took" or "refused", one word per path, releasing each.
func helper(mode string, paths []string) string {
	if mode != "locks" {
		return "unknown mode"
	}
	var out []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		base := filepath.Base(p)
		if base == ".parentlock" || base == "parent.lock" {
			out = append(out, takeThunderbirdLock(p))
			continue
		}
		l, err := lockfile.AcquireWait(p, "", 0)
		if err != nil {
			out = append(out, "refused")
			continue
		}
		l.Release()
		out = append(out, "took")
	}
	return strings.Join(out, " ")
}
