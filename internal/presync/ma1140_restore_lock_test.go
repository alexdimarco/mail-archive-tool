package presync

import (
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/tbrestore"
	"mail-archive-tool/internal/thunderbird"
)

// covers: MA-1140, R40, R31, S71
// The MailArchive restore lock seen by presync (docs/design-restore-thunderbird.md
// §5.2 note item 3; slice R3): while a restore into a profile holds its restore lock,
// presync's running check reports that profile in use, so the pre-sync starts nothing
// on it; another profile is not affected; once the restore releases the lock, the
// check passes again (positive twin). The lock lives in MailArchive's default settings
// folder, job.Dir(), under thunderbird.ProfileKey's name, and a second restore into the
// profile is refused with W-RESTORING. Presync's own rows (MA-553) stay as they were.
func TestPresyncHonoursTheRestoreLock(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("HOME", cfg)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(cfg, ".config"))
	t.Setenv("APPDATA", filepath.Join(cfg, "AppData"))
	profile := t.TempDir()
	other := t.TempDir()

	e := newFakeEnv()
	e.realRestore = true
	if got := detectRunning(e, profile); got.running {
		t.Fatalf("no restore: not in use (positive twin): %+v", got)
	}

	rl, err := tbrestore.AcquireRestoreLock(profile)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, thunderbird.RestoreLockName(thunderbird.ProfileKey(profile)))
	if got, _ := thunderbird.RestoreLockPath(profile); got != want {
		t.Fatalf("the restore lock lives in job.Dir(): %s, want %s", got, want)
	}

	got := detectRunning(e, profile)
	if !got.running || !got.byRestore || got.byLock || got.byProcess {
		t.Fatalf("a restore holding its lock makes the profile in use: %+v", got)
	}
	if got := detectRunning(e, other); got.running {
		t.Fatalf("another profile is not affected: %+v", got)
	}
	if !tbrestore.RestoreLockHeld(profile) {
		t.Fatal("the restore's own probe sees it")
	}
	_, err = tbrestore.AcquireRestoreLock(profile)
	if tbrestore.RefusalCode(err) != "W-RESTORING" {
		t.Fatalf("a second restore into the profile: W-RESTORING, got %v", err)
	}

	rl.Release()
	rl.Release()
	if got := detectRunning(e, profile); got.running {
		t.Fatalf("released: not in use: %+v", got)
	}
}
