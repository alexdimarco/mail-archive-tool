package desktop

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1015, R36
// The Scheduled backup card's Install and Repair are refused for the current archive whose delete
// did not finish (the 1.5.3 re-walk's adversarial pass, F2): a delete of the current archive that
// stopped at step 2 leaves it current, and Install or Repair would put back the scheduled backup
// the delete was stopping — whose next run passes the emptied-folder guard once the delete has gone
// further, since the manifest goes last. MA-979's hold refused them only while a delete runs. Now
// /api/schedule-install refuses, right after that hold, in MA-987's words; nothing is installed.
// Positive twin first: Install works for another current archive with that unfinished delete in
// the same log.
func TestInstallAndRepairOfACurrentArchiveWhoseDeleteDidNotFinishAreRefused(t *testing.T) {
	e := newDeleteEnv(t)
	installs := 0
	e.cfg.InstallBackup = func(schedule.Spec) error { installs++; return nil }
	e.d.cfg = e.cfg
	cur := e.plainArchive("Current")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	stopAt2(t, e, cur) // a delete of the current archive stopped at step 2: it is still current
	if e.d.cfg.effectiveOut() != cur {
		t.Fatalf("precondition: the stopped archive is still current, got %q", e.d.cfg.effectiveOut())
	}

	// Positive twin: another current archive, with that unfinished delete in the same log.
	other := e.plainArchive("Other")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: other}); err != nil {
		t.Fatal(err)
	}
	if _, m := e.post("/api/schedule-install", map[string]any{"interval": "weekly", "time": "03:00", "drawnFor": other}); m["ok"] != true || installs != 1 {
		t.Fatalf("positive twin: Install of a current archive with nothing waiting: %v (installs %d)", m, installs)
	}

	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]map[string]any{
		"Install": {"interval": "weekly", "time": "03:00", "drawnFor": cur},
		"Repair":  {"drawnFor": cur},
	} {
		code, m := e.post("/api/schedule-install", body)
		assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK),
			assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so its scheduled backup was not installed; nothing was changed."),
			assure.NoSideEffect(func() bool { return m["ok"] == false && installs == 1 }))
		// The Settings card quotes the reason inside "…saved. The backup schedule was not changed:
		// REASON": it never says "nothing was changed" there (the re-walk's guards pass, G7).
		if m["reason"] != "Delete not finished: click Delete again. This archive's delete did not finish, so its scheduled backup was not installed." {
			t.Errorf("%s: the reason the Settings card quotes: %q", name, m["reason"])
		}
		if t.Failed() {
			t.Fatalf("%s: %v", name, m)
		}
	}
}

// covers: MA-1015, R36
// A deletions.log that cannot be read refuses the Scheduled backup card's Install, naming it,
// since whether the current archive's delete finished cannot be told; nothing is installed.
// Positive twin first: with the log readable, the same Install works.
func TestInstallRefusesADeletionLogItCannotRead(t *testing.T) {
	e := newDeleteEnv(t)
	installs := 0
	e.cfg.InstallBackup = func(schedule.Spec) error { installs++; return nil }
	e.d.cfg = e.cfg
	cur := e.plainArchive("Current")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"interval": "weekly", "time": "03:00", "drawnFor": cur}
	if _, m := e.post("/api/schedule-install", body); m["ok"] != true || installs != 1 {
		t.Fatalf("positive twin: Install with a readable log: %v (installs %d)", m, installs)
	}

	logPath := e.cfg.deletionsLogPath()
	_ = os.Remove(logPath)
	if err := os.MkdirAll(logPath, 0o700); err != nil { // a folder where the file goes
		t.Fatal(err)
	}
	code, m := e.post("/api/schedule-install", body)
	assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK),
		assure.Names("MailArchive could not read its deletion log, "+logPath, "so it cannot tell whether a delete of this archive did not finish; nothing was changed. Check that file, then try again."),
		assure.NoSideEffect(func() bool { return m["ok"] == false && installs == 1 }))
	if r, _ := m["reason"].(string); !strings.Contains(r, "did not finish. Check that file, then try again.") || strings.Contains(strings.ToLower(r), "nothing was") {
		t.Errorf("the reason the Settings card quotes: %q", m["reason"])
	}
}
