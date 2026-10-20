package schedule

import (
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func entryOut(es []InstalledEntry, name string) (string, bool) {
	for _, e := range es {
		if e.Name == name {
			return e.Out, true
		}
	}
	return "", false
}

// covers: MA-673
// The uninstall schedule sweep finds every mailarchive-owned entry by our OWN namespace —
// cron marker blocks (backup, a custom name, and the separate verify), launchd plists, and
// Windows side-files — not by a known name, so a CLI-created schedule the index never
// recorded is still found; foreign entries are ignored (U-C3, SD-6).
func TestMA673_SweepFindsOurEntriesByNamespace(t *testing.T) {
	// cron: a foreign comment is ignored; our default + custom + verify blocks are found
	// with their -out recovered.
	tab := "# some user job\n0 1 * * * /bin/true\n" +
		"# mailarchive-backup\n0 3 * * 0 mailarchive -out /a -mode incremental\n" +
		"# mailarchive-backup weekly-b\n0 4 * * 0 mailarchive -out /b\n" +
		"# mailarchive-backup weekly-b-verify\n0 5 * * 0 mailarchive verify -out /b\n" +
		"# mailarchive-backupX not-ours\n0 6 * * 0 something\n"
	got := ourCron(tab)
	if len(got) != 3 {
		t.Fatalf("want 3 of our cron blocks (backup, weekly-b, weekly-b-verify), got %d: %+v", len(got), got)
	}
	for name, wantOut := range map[string]string{"mailarchive-backup": "/a", "weekly-b": "/b", "weekly-b-verify": "/b"} {
		o, ok := entryOut(got, name)
		if !ok {
			t.Errorf("cron sweep missed %q", name)
		} else if o != wantOut {
			t.Errorf("cron entry %q out = %q, want %q", name, o, wantOut)
		}
	}

	// launchd: our plists under $HOME/Library/LaunchAgents are found; a foreign one is not.
	home := t.TempDir()
	t.Setenv("HOME", home)
	la := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(la, 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Join(la, "com.dimarcotech.mailarchive.mailarchive-backup.plist"))
	touch(t, filepath.Join(la, "com.dimarcotech.mailarchive.weekly-b-verify.plist"))
	touch(t, filepath.Join(la, "com.apple.something.plist")) // foreign
	lgot := ourLaunchd()
	if len(lgot) != 2 {
		t.Fatalf("want 2 of our launchd plists, got %d: %+v", len(lgot), lgot)
	}
	if _, ok := entryOut(lgot, "mailarchive-backup"); !ok {
		t.Error("launchd sweep missed the backup plist")
	}
	if _, ok := entryOut(lgot, "weekly-b-verify"); !ok {
		t.Error("launchd sweep missed the verify plist")
	}

	// Windows schtasks side-files under %LOCALAPPDATA%\mailarchive (<name>.cmd/.xml), deduped.
	lad := t.TempDir()
	t.Setenv("LOCALAPPDATA", lad)
	md := filepath.Join(lad, "mailarchive")
	if err := os.MkdirAll(md, 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Join(md, "mailarchive-backup.xml"))
	touch(t, filepath.Join(md, "mailarchive-backup.cmd")) // same name, deduped
	touch(t, filepath.Join(md, "weekly-b.xml"))
	touch(t, filepath.Join(md, "notes.txt")) // foreign extension
	sgot := ourSchtasks()
	if len(sgot) != 2 {
		t.Fatalf("want 2 deduped schtasks side-file names, got %d: %+v", len(sgot), sgot)
	}
}
