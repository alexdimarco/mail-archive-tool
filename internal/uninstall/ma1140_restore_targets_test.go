package uninstall

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/tbrestore"
	"mail-archive-tool/internal/thunderbird"
)

// covers: MA-1140, R40, S71
// Uninstall's settings-file targets include Restore to Thunderbird's record and locks
// (docs/design-restore-thunderbird.md §7.4 foldcheck2 note N-b), kept in step with the
// restore's own names by behaviour: restores.log as tbrestore writes it and a lock
// tbrestore.AcquireRestoreLock made, both in MailArchive's default settings folder
// (job.Dir(), which is where the restore puts them whatever -config says), are targets;
// "restore-x.lock", "restore-<16 hex>.lock.bak" and a folder of a lock's name are not.
// The plan names the leftovers the restore's display rule gives (uninstall removes none).
func TestUninstallTargetsTheRestoreRecordAndLocks(t *testing.T) {
	cfg, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	logPath, err := tbrestore.LogPath()
	if err != nil || filepath.Dir(logPath) != cfg {
		t.Fatalf("restores.log is in job.Dir(): %s %v", logPath, err)
	}
	if err := tbrestore.NewLog(logPath).Append(tbrestore.LogLine{Event: tbrestore.EvStarted, ID: "abc"}); err != nil {
		t.Fatal(err)
	}
	profile := t.TempDir()
	rl, err := tbrestore.AcquireRestoreLock(profile)
	if err != nil {
		t.Fatal(err)
	}
	rl.Release()
	lock := filepath.Join(cfg, thunderbird.RestoreLockName(thunderbird.ProfileKey(profile)))
	for _, n := range []string{"restore-x.lock", "restore-0123456789abcdef.lock.bak"} {
		if err := os.WriteFile(filepath.Join(cfg, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(cfg, "restore-fedcba9876543210.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	targets, err := ConfigTargets(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{logPath, lock} {
		if !slices.Contains(targets, want) {
			t.Errorf("%s is a target: %v", want, targets)
		}
	}
	for _, not := range []string{"restore-x.lock", "restore-0123456789abcdef.lock.bak", "restore-fedcba9876543210.lock"} {
		if slices.Contains(targets, filepath.Join(cfg, not)) {
			t.Errorf("%s is not a target", not)
		}
	}

	was := restoreLeftovers
	restoreLeftovers = func() []string { return []string{"An earlier restore into Thunderbird did not finish (DATE)."} }
	defer func() { restoreLeftovers = was }()
	rep, err := Uninstall(cfg, nil, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.RestoreLeftovers) != 1 {
		t.Fatalf("the plan names the restore's leftovers: %v", rep.RestoreLeftovers)
	}
	restoreLeftovers = func() []string { return nil }
	if rep, _ := Uninstall(cfg, nil, "", true); len(rep.RestoreLeftovers) != 0 {
		t.Fatal("positive twin: none named when there are none")
	}
}
