package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/vault"
)

// covers: MA-834, R37
// The Desktop's schedule refuses a program kept in an encrypted archive before any
// schedule is built (program design §6, C15): backupSpec returns ErrKeptCopy and no
// spec. The healthy twin — a program outside any archive — builds the spec.
func TestTheDesktopNeverSchedulesAKeptCopy(t *testing.T) {
	orig := schedule.SelfResolver
	t.Cleanup(func() { schedule.SelfResolver = orig })
	out, dir := t.TempDir(), t.TempDir()
	sp := filepath.Join(dir, "settings.json")
	if err := SaveSettings(sp, Settings{Out: out, Interval: "weekly", WeeklyTime: "02:30"}); err != nil {
		t.Fatal(err)
	}
	cfg := Config{ConfigPath: filepath.Join(dir, "graph-config.json"), SettingsPath: sp}
	schedule.SelfResolver = func() (string, error) { return filepath.Join(t.TempDir(), "mailarchive"), nil }
	if _, err := cfg.backupSpec(); err != nil {
		t.Fatalf("healthy twin: %v", err)
	}
	archive := t.TempDir()
	os.WriteFile(filepath.Join(archive, vault.FileName), []byte("{}"), 0o600)
	schedule.SelfResolver = func() (string, error) {
		return filepath.Join(archive, "program", "MailArchive.app", "Contents", "MacOS", "mailarchive"), nil
	}
	spec, err := cfg.backupSpec()
	if !errors.Is(err, schedule.ErrKeptCopy) || spec.Exe != "" {
		t.Fatalf("a kept copy: err %v, spec %q; want ErrKeptCopy and no spec", err, spec.Exe)
	}
}
