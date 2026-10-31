package uninstall

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/presyncjob"
	"mail-archive-tool/internal/schedule"
)

// fillPresyncState writes, in dir, what the Thunderbird sync keeps there: one profile's
// state record and session lock, and the first-attended-sync marker.
func fillPresyncState(t *testing.T, dir string) []string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, n := range []string{"presync-9f86d081884c7d65.json", "presync-9f86d081884c7d65.lock", "attended-sync-started"} {
		p := filepath.Join(dir, n)
		writeFile(t, p, "x")
		files = append(files, p)
	}
	return files
}

func sorted(s []string) []string {
	c := append([]string(nil), s...)
	sort.Strings(c)
	return c
}

// covers: MA-910, S65
// Where the pre-sync state folder is, per platform (design-presync-attended §5.5, C-63):
// the platform is passed to the builder with the two folders it is built from, so both
// choices are checked on any machine. macOS: the job folder (job.Dir) + presync; Windows:
// the local, non-roaming instance folder (instance.Dir, %LOCALAPPDATA%\MailArchiveDesktop)
// + presync, never the roaming job folder; a folder that cannot be found is an error; any
// other platform has none. On this machine the builder, with the real folders, agrees
// with presyncjob.StateDir, the folder the sync really writes (on the Windows lab VM this
// is the Windows location itself).
func TestThePresyncStateFolderIsFoundPerPlatform(t *testing.T) {
	jobDir := filepath.Join(t.TempDir(), "config", "mailarchive")
	instDir := filepath.Join(t.TempDir(), "local", "MailArchiveDesktop")
	jd := func() (string, error) { return jobDir, nil }
	id := func() (string, error) { return instDir, nil }
	if d, err := presyncStateDirFor("darwin", jd, id); err != nil || d != filepath.Join(jobDir, "presync") {
		t.Fatalf("darwin: %q (%v)", d, err)
	}
	if d, err := presyncStateDirFor("windows", jd, id); err != nil || d != filepath.Join(instDir, "presync") {
		t.Fatalf("windows: %q (%v)", d, err)
	}
	none := func() (string, error) { return "", errors.New("the local app-data folder is not set") }
	if d, err := presyncStateDirFor("windows", jd, none); err == nil || d != "" {
		t.Fatalf("windows with no instance folder: %q (%v)", d, err)
	}
	if d, err := presyncStateDirFor("linux", jd, id); err != nil || d != "" {
		t.Fatalf("linux: %q (%v)", d, err)
	}

	want, werr := presyncjob.StateDir()
	got, gerr := presyncStateDirFor(runtime.GOOS, job.Dir, instance.Dir)
	if werr != nil {
		if got != "" {
			t.Fatalf("this platform has no sync state folder, but uninstall names %q", got)
		}
		return
	}
	if gerr != nil || got != want {
		t.Fatalf("on %s uninstall looks in %q (%v); the sync writes to %q", runtime.GOOS, got, gerr, want)
	}
}

// covers: MA-910, S65
// What uninstall removes of the pre-sync state folder (C-40, C-63): its named files —
// each profile's state record and lock, and the first-sync marker — then the folder, last.
// It is checked at the Windows location too (the platform passed to the builder).
// Named files only, never a recursion: a file of any other name, a folder and a link
// named like ours are kept, and the folder is then kept too (it would not be empty). A
// lock another run holds right now is kept with its profile's record. An absent folder
// is nothing to remove; a folder that is, holds or sits in an archive refuses (R36).
func TestUninstallRemovesThePresyncStateFolder(t *testing.T) {
	for _, goos := range []string{"darwin", "windows"} {
		jobDir := filepath.Join(t.TempDir(), "config", "mailarchive")
		instDir := filepath.Join(t.TempDir(), "local", "MailArchiveDesktop")
		dir, err := presyncStateDirFor(goos, func() (string, error) { return jobDir, nil }, func() (string, error) { return instDir, nil })
		if err != nil {
			t.Fatal(err)
		}
		files := fillPresyncState(t, dir)
		got, err := PresyncStateTargets(dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := append(sorted(files), dir)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("%s: targets %v, want %v (the folder last)", goos, got, want)
		}
	}

	dir := filepath.Join(t.TempDir(), "presync")
	files := fillPresyncState(t, dir)
	writeFile(t, filepath.Join(dir, "notes.txt"), "someone's")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "sub", "presync-1.json"), "x")
	if runtime.GOOS != "windows" {
		elsewhere := filepath.Join(t.TempDir(), "victim")
		writeFile(t, elsewhere, "keep")
		if err := os.Symlink(elsewhere, filepath.Join(dir, "presync-abc.json")); err != nil {
			t.Fatal(err)
		}
	}
	got, err := PresyncStateTargets(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != strings.Join(sorted(files), "|") {
		t.Fatalf("with other things in the folder: targets %v, want only %v (and not the folder)", got, sorted(files))
	}

	held := filepath.Join(t.TempDir(), "presync")
	files = fillPresyncState(t, held)
	l, err := lockfile.Acquire(filepath.Join(held, "presync-9f86d081884c7d65.lock"))
	if err != nil {
		t.Fatal(err)
	}
	got, err = PresyncStateTargets(held, nil)
	_ = l.Release()
	if err != nil || len(got) != 1 || got[0] != filepath.Join(held, "attended-sync-started") {
		t.Fatalf("a held lock: targets %v (%v), want only the marker", got, err)
	}

	if got, err := PresyncStateTargets(filepath.Join(t.TempDir(), "absent"), nil); err != nil || len(got) != 0 {
		t.Fatalf("an absent folder: %v (%v)", got, err)
	}

	arch := t.TempDir()
	writeFile(t, filepath.Join(arch, manifestName), `{"version":6,"stores":{}}`)
	inside := filepath.Join(arch, "presync")
	fillPresyncState(t, inside)
	if _, err := PresyncStateTargets(inside, []string{arch}); !errors.Is(err, ErrOverlap) {
		t.Fatalf("a folder inside a known archive: %v, want ErrOverlap", err)
	}
	marked := filepath.Join(t.TempDir(), "presync")
	fillPresyncState(t, marked)
	writeFile(t, filepath.Join(marked, manifestName), `{"version":6,"stores":{}}`)
	if _, err := PresyncStateTargets(marked, nil); !errors.Is(err, ErrOverlap) {
		t.Fatalf("a folder holding an archive's manifest: %v, want ErrOverlap", err)
	}
}

// covers: MA-910, S65
// Through Uninstall: a dry run lists the pre-sync state with the settings files and
// removes nothing; a real run removes the named files and then the folder, and keeps the
// archive (R36).
func TestUninstallRunRemovesThePresyncState(t *testing.T) {
	origList, origRemove, origClear, origDir := listSchedules, removeSchedule, clearSignIn, presyncStateDir
	defer func() {
		listSchedules, removeSchedule, clearSignIn, presyncStateDir = origList, origRemove, origClear, origDir
	}()
	listSchedules = func() []schedule.InstalledEntry { return nil }
	removeSchedule = func(schedule.Spec) error { return nil }
	clearSignIn = func(string, string) error { return nil }
	dir := filepath.Join(t.TempDir(), "presync")
	presyncStateDir = func() (string, error) { return dir, nil }
	files := fillPresyncState(t, dir)

	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "desktop-settings.json"), "x")
	kept := t.TempDir()
	writeFile(t, filepath.Join(kept, manifestName), `{"version":6,"stores":{}}`)

	rep, err := Uninstall(cfg, []string{kept}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	listed := strings.Join(rep.ConfigFiles, "|")
	for _, f := range append(files, dir) {
		if !strings.Contains(listed, f) {
			t.Errorf("the dry run does not list %s: %v", f, rep.ConfigFiles)
		}
	}
	if _, err := os.Stat(files[0]); err != nil {
		t.Fatal("a dry run removed the sync's state")
	}
	if _, err := Uninstall(cfg, []string{kept}, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("the real run left the sync's state folder (%v)", err)
	}
	if _, err := os.Stat(filepath.Join(kept, manifestName)); err != nil {
		t.Fatal("uninstall must NEVER delete an archive (R36)")
	}
}
