package tbrestore

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1085, R40, S73
// R6-07 (settled 2026-11-07): the Local Folders actually written is held to the profile's
// own rule on macOS and Linux. Prefs pointing Local Folders at a folder outside the
// profile that other accounts can change (0777) is refused W-LOCALMODE naming it and the
// remedy, nothing changed; positive twin: the same folder at 0700 is found.
func TestALocalFoldersOthersCanChangeIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ownership and mode are not checked on Windows (design §9 item 4); R6-01's owner-only files cover the read side there")
	}
	shared := filepath.Join(t.TempDir(), "shared")
	if err := os.MkdirAll(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	prof := makeProfile(t, filepath.Join(t.TempDir(), "p.default"),
		`user_pref("mail.accountmanager.localfoldersserver", "server1");`,
		`user_pref("mail.server.server1.directory", "`+jsEscape(shared)+`");`,
		`user_pref("mail.server.server1.type", "none");`)
	lf, err := LocalFoldersOf(prof)
	assure.Reached(t, err == nil && lf.Dir == canonical(shared), "positive twin: a Local Folders of this user's alone is found")

	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, shared)
	_, err = LocalFoldersOf(prof)
	refusedAs(t, err, "W-LOCALMODE", func() bool { return snapshot(t, shared) == before }, canonical(shared), "can be changed by other accounts", "chmod go-w")
}

// covers: MA-1140, R40
// R6-15 (settled 2026-11-07): one profile reached by a second spelling no link resolves
// (macOS: its /System/Volumes/Data firmlink) has one restore lock: held under one
// spelling, it is held under the other, so a second restore is told W-RESTORING and
// presync starts nothing. Off macOS a symbolic link stands in (MA-1140's own case).
func TestOneRestoreLockForOneProfileHoweverSpelled(t *testing.T) {
	home := t.TempDir() // MailArchive's settings folder, where the restore locks live, is a throwaway
	for _, v := range []string{"HOME", "XDG_CONFIG_HOME", "APPDATA"} {
		t.Setenv(v, home)
	}
	prof, _ := localProfile(t)
	alias := filepath.Join(t.TempDir(), "link")
	switch runtime.GOOS {
	case "darwin":
		alias = "/System/Volumes/Data" + prof
		if _, err := os.Stat(alias); err != nil {
			t.Skipf("no firmlink spelling here: %v", err)
		}
	case "windows":
		t.Skip("the junction spelling is TestProfileKeyFollowsTheFolderNotItsSpelling's Windows leg (internal/thunderbird)")
	default:
		if err := os.Symlink(prof, alias); err != nil {
			t.Fatal(err)
		}
	}
	assure.Reached(t, !RestoreLockHeld(alias), "positive twin: not held before")
	l, err := AcquireRestoreLock(prof)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	assure.Reached(t, RestoreLockHeld(alias), "held under "+prof+", it is held under "+alias)
	_, err = AcquireRestoreLock(alias)
	refusedAs(t, err, "W-RESTORING", nil, "Another restore into this Thunderbird profile is running")
}
