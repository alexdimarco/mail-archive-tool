package desktop

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// writeCaptureDescriptor records an installed capture for the folder out at iv and at, as
// schedule.Install would, without touching any scheduler.
func writeCaptureDescriptor(t *testing.T, out string, iv schedule.Interval, at string) {
	t.Helper()
	name := schedule.DefaultNameFor(out)
	spec := schedule.Spec{Name: name, Interval: iv, At: at, Exe: "/opt/mailarchive",
		Args: []string{"-capture", "-config", "c.json", "-out", out, "-log", "l.log"}, Out: out}
	if err := schedule.WriteDescriptor(out, schedule.DescribeSpec(spec)); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-860, MA-866, R37
// A capture install builds from the cadence posted, never from the settings file, and the
// settings file records it only after the install succeeded (C-13, R3C-14): a refused install
// leaves the settings file as it was, and an unknown interval is refused before anything is
// written. Positive twin first: a successful install is handed exactly the posted cadence and
// recorded.
func TestACaptureInstallsThePostedCadenceAndRecordsItOnlyAfterSuccess(t *testing.T) {
	e := newWsEnv(t)
	fake := &backupFake{tasks: map[string]bool{}}
	useBackupFake(e, fake)
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: e.out, Interval: "weekly", WeeklyTime: "03:00"}); err != nil {
		t.Fatal(err)
	}
	installs := seamCaptureInstall(e, fake)
	if _, body := e.post("/api/schedule-install", map[string]any{"interval": "daily", "time": "05:15"}); body["ok"] != true {
		t.Fatalf("the positive twin: %v", body)
	}
	if len(*installs) != 1 || (*installs)[0].Interval != schedule.Daily || (*installs)[0].At != "05:15" {
		t.Fatalf("the install was not handed the posted cadence: %+v", *installs)
	}
	if s := LoadSettings(e.cfg.SettingsPath); s.Interval != "daily" || s.WeeklyTime != "05:15" {
		t.Errorf("the installed cadence was not recorded: %q at %q", s.Interval, s.WeeklyTime)
	}

	// A refused install: the settings file is as it was.
	e.cfg.InstallBackup = func(schedule.Spec) error { return errors.New("the scheduler said no") }
	e.d.cfg = e.cfg
	before := fileBytes(e.cfg.SettingsPath)
	_, body := e.post("/api/schedule-install", map[string]any{"interval": "weekly", "time": "06:00"})
	msg, _ := body["error"].(string)
	rc := 0
	if body["ok"] == false {
		rc = 2
	}
	assure.Refused(t, rc, msg, assure.Names("the scheduler said no"),
		assure.NoSideEffect(func() bool { return bytes.Equal(before, fileBytes(e.cfg.SettingsPath)) }))

	// An unknown interval: refused before any install or write.
	installs = seamCaptureInstall(e, fake)
	_, body = e.post("/api/schedule-install", map[string]any{"interval": "fortnightly", "time": "06:00"})
	msg, _ = body["error"].(string)
	rc = 0
	if body["ok"] == false {
		rc = 2
	}
	assure.Refused(t, rc, msg, assure.Names("fortnightly", "Nothing was changed."),
		assure.NoSideEffect(func() bool { return len(*installs) == 0 && bytes.Equal(before, fileBytes(e.cfg.SettingsPath)) }))
}

// covers: MA-866, R37
// The installed cadence is recorded before the BackupFor clean-up, which can return early
// (R3C-14): when the earlier location's entry can't be removed the reply says so, and the
// settings file still records what was installed.
func TestACaptureRecordsItsCadenceBeforeTheCleanUpCanReturnEarly(t *testing.T) {
	e := newWsEnv(t)
	old := t.TempDir()
	fake := &backupFake{tasks: map[string]bool{old: true}}
	useBackupFake(e, fake)
	e.cfg.RemoveBackupFor = func(string) error { return errors.New("the scheduler is busy") }
	installs := seamCaptureInstall(e, fake)
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: e.out, BackupFor: old, Interval: "weekly", WeeklyTime: "03:00"}); err != nil {
		t.Fatal(err)
	}
	_, body := e.post("/api/schedule-install", map[string]any{"interval": "daily", "time": "07:30"})
	if body["ok"] != false || !strings.Contains(body["error"].(string), "could not be removed") {
		t.Fatalf("a clean-up that fails: %v", body)
	}
	if len(*installs) != 1 {
		t.Fatalf("installs %+v", *installs)
	}
	if s := LoadSettings(e.cfg.SettingsPath); s.Interval != "daily" || s.WeeklyTime != "07:30" {
		t.Errorf("the early return skipped recording the installed cadence: %q at %q", s.Interval, s.WeeklyTime)
	}
}

// covers: MA-866, R37
// A capture Repair (an empty body) keeps its own cadence (R3C-14): it builds from the
// installed descriptor's interval and time when the one settings file holds another
// archive's, and it re-points a BackupFor capture to the current location, building from the
// cadence that capture had. Before, it built from the settings file.
func TestACaptureRepairKeepsTheDescriptorCadence(t *testing.T) {
	e := newWsEnv(t)
	fake := &backupFake{tasks: map[string]bool{e.out: true}}
	useBackupFake(e, fake)
	installs := seamCaptureInstall(e, fake)
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: e.out, Interval: "weekly", WeeklyTime: "03:00"}); err != nil {
		t.Fatal(err)
	}
	writeCaptureDescriptor(t, e.out, schedule.Daily, "06:30")
	if _, body := e.post("/api/schedule-install", map[string]any{}); body["ok"] != true {
		t.Fatalf("Repair: %v", body)
	}
	if len(*installs) != 1 || (*installs)[0].Interval != schedule.Daily || (*installs)[0].At != "06:30" {
		t.Fatalf("Repair did not keep the descriptor's daily at 06:30: %+v", *installs)
	}

	// Re-point: the capture is named for the earlier location a, the archive is now b.
	a, b := t.TempDir(), t.TempDir()
	fake.tasks = map[string]bool{a: true}
	fake.removed = nil
	*installs = nil
	writeCaptureDescriptor(t, a, schedule.Daily, "07:45")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: b, BackupFor: a, Interval: "weekly", WeeklyTime: "03:00"}); err != nil {
		t.Fatal(err)
	}
	if _, body := e.post("/api/schedule-install", map[string]any{}); body["ok"] != true {
		t.Fatalf("Repair after Save and keep: %v", body)
	}
	if len(*installs) != 1 || (*installs)[0].Out != b || (*installs)[0].Interval != schedule.Daily || (*installs)[0].At != "07:45" {
		t.Fatalf("Repair did not re-point the capture to %s at its daily 07:45: %+v", b, *installs)
	}
	if fake.tasks[a] || len(fake.removed) != 1 || fake.removed[0] != a {
		t.Errorf("the earlier location's entry was not removed: tasks %v removed %v", fake.tasks, fake.removed)
	}
	if s := LoadSettings(e.cfg.SettingsPath); s.BackupFor != "" {
		t.Errorf("BackupFor %q after the re-point", s.BackupFor)
	}
}
