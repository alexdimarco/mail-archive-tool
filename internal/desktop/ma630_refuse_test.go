package desktop

import (
	"errors"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-630, R18, S59
// A translocated program is refused before any schedule is built: backupSpecFor
// (the Weekly backup card's and §7's caller) returns ErrTranslocated and no spec,
// so nothing is ever handed to the scheduler. The healthy twin — a normal program
// — builds the spec. The refusal happens before job/scheduler side effects because
// ProgramForSchedule is consulted first (§4.6.1, MA-630).
func TestMA630_BackupRefusesTranslocatedProgram(t *testing.T) {
	orig := schedule.SelfResolver
	t.Cleanup(func() { schedule.SelfResolver = orig })

	out, dir := t.TempDir(), t.TempDir()
	sp := filepath.Join(dir, "settings.json")
	if err := SaveSettings(sp, Settings{Out: out, Interval: "weekly", WeeklyTime: "02:30"}); err != nil {
		t.Fatal(err)
	}
	// Exe empty, so backupSpecFor resolves the program through ProgramForSchedule.
	cfg := Config{ConfigPath: filepath.Join(dir, "graph-config.json"), SettingsPath: sp}

	// Healthy twin first: a normal program resolves and a spec is built.
	schedule.SelfResolver = func() (string, error) { return filepath.Join(t.TempDir(), "mailarchive"), nil }
	if _, err := cfg.backupSpec(); err != nil {
		t.Fatalf("healthy twin: backupSpec returned %v, want a spec", err)
	}

	// Translocated program: refused with the X2 message, and no spec is produced.
	schedule.SelfResolver = func() (string, error) {
		return "/private/var/folders/aa/bb/C/AppTranslocation/DEAD-BEEF/d/Mail Archive.app/Contents/MacOS/mailarchive", nil
	}
	spec, err := cfg.backupSpec()
	if !errors.Is(err, schedule.ErrTranslocated) {
		t.Fatalf("translocated: err = %v, want ErrTranslocated", err)
	}
	if spec.Exe != "" {
		t.Errorf("translocated: a spec was built (Exe %q); nothing should reach the scheduler", spec.Exe)
	}
}
