package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// plantRunJob writes the run-job job file for the archive folder a, as the wizard and the
// Desktop name it (job.PathFor(DefaultNameFor(a))), and returns its path.
func plantRunJob(t *testing.T, a string) string {
	t.Helper()
	p, err := job.PathFor(schedule.DefaultNameFor(a))
	if err != nil {
		t.Fatal(err)
	}
	if err := job.Write(p, job.Job{Name: schedule.DefaultNameFor(a), Inputs: []string{t.TempDir()}, Out: a, Mode: "incremental"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(p) })
	return p
}

// seamCaptureInstall makes the capture install record into the fake scheduler, never the OS's.
func seamCaptureInstall(e *wsEnv, fake *backupFake) *[]schedule.Spec {
	var got []schedule.Spec
	e.cfg.InstallBackup = func(s schedule.Spec) error {
		got = append(got, s)
		fake.tasks[s.Out] = true
		return nil
	}
	e.d.cfg = e.cfg
	return &got
}

// covers: MA-862, R37
// A location change with a run-job installed names the old folder and says its folder is fixed
// (section 8, C-3); "Save and keep" sets no BackupFor, so the new archive's card, tile and
// schedule state show no backup, and its Remove and its capture install leave the run-job's
// entry and job file alone. Positive twin first: a capture keeps MA-523's behaviour (its words,
// and keep records BackupFor).
func TestARunJobLocationChangeNamesTheOldFolderAndKeepSetsNoBackupFor(t *testing.T) {
	e := newWsEnv(t)
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()

	// Positive twin: a capture installed for c (no job file).
	fake := &backupFake{tasks: map[string]bool{c: true}}
	useBackupFake(e, fake)
	if err := UpdateSettings(e.cfg.SettingsPath, "test", func(s *Settings) { s.Out = c }); err != nil {
		t.Fatal(err)
	}
	_, ask := e.post("/api/settings", map[string]any{"out": b})
	if ask["confirm"] != true || !strings.Contains(ask["error"].(string), "will now archive") {
		t.Fatalf("a capture's question changed: %v", ask)
	}
	if _, body := e.post("/api/settings", map[string]any{"out": b, "backup": "keep"}); body["ok"] != true || LoadSettings(e.cfg.SettingsPath).BackupFor != c {
		t.Fatalf("a capture's keep no longer records BackupFor: %v, %q", body, LoadSettings(e.cfg.SettingsPath).BackupFor)
	}

	// The run-job: installed for a, its job file names a.
	jp := plantRunJob(t, a)
	fake.tasks = map[string]bool{a: true}
	fake.removed = nil
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: a}); err != nil {
		t.Fatal(err)
	}
	_, ask = e.post("/api/settings", map[string]any{"out": b})
	msg, _ := ask["error"].(string)
	if ask["confirm"] != true || !strings.Contains(msg, "keeps backing up "+a) || !strings.Contains(msg, "fixed in its own settings file") || strings.Contains(msg, "will now archive") {
		t.Fatalf("a run-job's location question: %v", ask)
	}
	if _, body := e.post("/api/settings", map[string]any{"out": b, "backup": "keep"}); body["ok"] != true {
		t.Fatalf("Save and keep: %v", body)
	}
	if s := LoadSettings(e.cfg.SettingsPath); s.Out != b || s.BackupFor != "" {
		t.Fatalf("after keep: out %q, BackupFor %q; want %s and no BackupFor", s.Out, s.BackupFor, b)
	}
	if got := weeklyBackupCard(e); got == "Installed" || e.d.backupInstalled() {
		t.Errorf("B's card says %q (installed %v): A's run-job is shown as B's backup", got, e.d.backupInstalled())
	}
	if _, st := e.post("/api/schedule-state", nil); st["state"] == "installed" {
		t.Errorf("B's schedule state shows A's run-job: %v", st)
	}
	if _, out := e.post("/api/schedule-remove", nil); out["ok"] != true {
		t.Fatalf("B's Remove: %v", out)
	}
	installs := seamCaptureInstall(e, fake)
	if _, out := e.post("/api/schedule-install", map[string]any{"interval": "weekly", "time": "03:00"}); out["ok"] != true {
		t.Fatalf("B's capture install: %v", out)
	}
	if len(*installs) != 1 || (*installs)[0].Out != b {
		t.Fatalf("B's capture install was handed %+v", *installs)
	}
	if !fake.tasks[a] || len(fake.removed) != 0 {
		t.Errorf("B's Remove or capture install removed A's run-job entry: tasks %v, removed %v", fake.tasks, fake.removed)
	}
	if _, err := os.Stat(jp); err != nil {
		t.Errorf("A's job file is gone: %v", err)
	}
}

// covers: MA-862, R37
// "Save and remove" on a run-job removes its entry and its job file, as Remove on the card
// does, so no dead job file is left for A.
func TestARunJobLocationChangeRemoveLeavesNoJobFile(t *testing.T) {
	e := newWsEnv(t)
	a, b := t.TempDir(), t.TempDir()
	jp := plantRunJob(t, a)
	fake := &backupFake{tasks: map[string]bool{a: true}}
	useBackupFake(e, fake)
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: a}); err != nil {
		t.Fatal(err)
	}
	if _, body := e.post("/api/settings", map[string]any{"out": b, "backup": "remove"}); body["ok"] != true {
		t.Fatalf("Save and remove: %v", body)
	}
	if fake.tasks[a] || len(fake.removed) != 1 || fake.removed[0] != a {
		t.Errorf("A's entry was not removed: tasks %v, removed %v", fake.tasks, fake.removed)
	}
	if _, err := os.Stat(jp); !os.IsNotExist(err) {
		t.Errorf("A's job file was left behind: %v", err)
	}
	if s := LoadSettings(e.cfg.SettingsPath); s.Out != b || s.BackupFor != "" {
		t.Errorf("after remove: out %q, BackupFor %q", s.Out, s.BackupFor)
	}
}

// covers: MA-862, R37
// The start-up clean-up (section 8, rev4.1): a BackupFor an earlier version recorded for a
// run-job's folder (its job file reads securely with Out = that folder) is cleared, and only
// that field changes; after it, B's Remove and B's capture install leave A's entry and job file.
// Positive twin first: a capture's BackupFor (no job file) is kept, the settings file untouched;
// and a job file that can't be read keeps the BackupFor (the stated residual).
func TestStartUpClearsABackupForThatNamesARunJob(t *testing.T) {
	e := newWsEnv(t)
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: b, BackupFor: c, KeepRaw: true, Interval: "daily", WeeklyTime: "04:00"}); err != nil {
		t.Fatal(err)
	}
	before := fileBytes(e.cfg.SettingsPath)
	if cleared, err := ClearRunJobBackupFor(e.cfg.SettingsPath); err != nil || cleared || !bytes.Equal(before, fileBytes(e.cfg.SettingsPath)) {
		t.Fatalf("the positive twin, a capture's BackupFor: cleared %v err %v, file changed %v", cleared, err, !bytes.Equal(before, fileBytes(e.cfg.SettingsPath)))
	}

	// The residual: an unreadable job file at A's name keeps the BackupFor.
	bad, err := job.PathFor(schedule.DefaultNameFor(a))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte(`{"version":99,"out":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: b, BackupFor: a, KeepRaw: true, Interval: "daily", WeeklyTime: "04:00"}); err != nil {
		t.Fatal(err)
	}
	if cleared, _ := ClearRunJobBackupFor(e.cfg.SettingsPath); cleared || LoadSettings(e.cfg.SettingsPath).BackupFor != a {
		t.Errorf("a BackupFor whose job file can't be read was cleared")
	}
	os.Remove(bad)

	jp := plantRunJob(t, a)
	cleared, err := ClearRunJobBackupFor(e.cfg.SettingsPath)
	if err != nil || !cleared {
		t.Fatalf("a run-job's BackupFor was not cleared: %v %v", cleared, err)
	}
	s := LoadSettings(e.cfg.SettingsPath)
	if s.BackupFor != "" || s.Out != b || !s.KeepRaw || s.Interval != "daily" || s.WeeklyTime != "04:00" {
		t.Errorf("the clean-up changed more than BackupFor: %+v", s)
	}

	fake := &backupFake{tasks: map[string]bool{a: true}}
	useBackupFake(e, fake)
	installs := seamCaptureInstall(e, fake)
	if _, out := e.post("/api/schedule-remove", nil); out["ok"] != true {
		t.Fatalf("B's Remove: %v", out)
	}
	if _, out := e.post("/api/schedule-install", map[string]any{"interval": "weekly", "time": "03:00"}); out["ok"] != true || len(*installs) != 1 {
		t.Fatalf("B's capture install: %v", out)
	}
	if !fake.tasks[a] || len(fake.removed) != 0 {
		t.Errorf("after the clean-up B's Remove or capture install removed A's entry: tasks %v removed %v", fake.tasks, fake.removed)
	}
	if _, err := os.Stat(jp); err != nil {
		t.Errorf("A's job file is gone: %v", err)
	}
}

// covers: MA-862, R37
// The clean-up runs when the Desktop starts: a launch whose settings file records a run-job's
// folder as BackupFor serves with it cleared. Positive twin first: a launch with a capture's
// BackupFor keeps it.
func TestTheDesktopStartClearsARunJobBackupFor(t *testing.T) {
	start := func(backupFor string) Settings {
		g := newDRig(t)
		sp := filepath.Join(filepath.Dir(g.cfgPath), "desktop-settings.json")
		if err := SaveSettings(sp, Settings{Out: g.out, BackupFor: backupFor}); err != nil {
			t.Fatal(err)
		}
		g.start()
		g.stop()
		return LoadSettings(sp)
	}
	capture := t.TempDir()
	if s := start(capture); s.BackupFor != capture {
		t.Fatalf("the positive twin: a capture's BackupFor was not kept at start (%q)", s.BackupFor)
	}
	a := t.TempDir()
	plantRunJob(t, a)
	s := start(a)
	if s.BackupFor != "" {
		t.Errorf("the Desktop started with a run-job's BackupFor still recorded: %q", s.BackupFor)
	}
	assure.Reached(t, s.Out, "the settings' archive location after start")
}

// covers: MA-862, R37
// A BackupFor written after start (by an older version) that names a run-job is skipped by
// backupTask, which writes nothing, and by Remove and the capture install's clean-up, which
// clear it before acting and leave A's entry and job file.
func TestABackupForWrittenAfterStartIsSkipped(t *testing.T) {
	e := newWsEnv(t)
	a, b := t.TempDir(), t.TempDir()
	jp := plantRunJob(t, a)
	fake := &backupFake{tasks: map[string]bool{a: true}}
	useBackupFake(e, fake)
	installs := seamCaptureInstall(e, fake)
	set := func() {
		if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: b, BackupFor: a}); err != nil {
			t.Fatal(err)
		}
	}

	set()
	if p, _ := e.d.cfg.backupTask(); p != "" {
		t.Errorf("backupTask shows A's run-job as B's backup: %q", p)
	}
	if got := weeklyBackupCard(e); got == "Installed" {
		t.Error("B's card shows A's run-job")
	}
	if LoadSettings(e.cfg.SettingsPath).BackupFor != a {
		t.Error("drawing the page wrote the settings file")
	}

	if _, out := e.post("/api/schedule-remove", nil); out["ok"] != true {
		t.Fatalf("B's Remove: %v", out)
	}
	if !fake.tasks[a] || len(fake.removed) != 0 {
		t.Errorf("B's Remove removed A's run-job entry: removed %v", fake.removed)
	}
	if LoadSettings(e.cfg.SettingsPath).BackupFor != "" {
		t.Error("Remove did not clear the BackupFor that names a run-job")
	}

	set()
	if _, out := e.post("/api/schedule-install", map[string]any{"interval": "daily", "time": "02:00"}); out["ok"] != true {
		t.Fatalf("B's capture install: %v", out)
	}
	if len(*installs) != 1 || !fake.tasks[a] || len(fake.removed) != 0 {
		t.Errorf("B's capture install removed A's run-job entry: installs %d, tasks %v, removed %v", len(*installs), fake.tasks, fake.removed)
	}
	if LoadSettings(e.cfg.SettingsPath).BackupFor != "" {
		t.Error("the capture install did not clear the BackupFor that names a run-job")
	}
	if _, err := os.Stat(jp); err != nil {
		t.Errorf("A's job file is gone: %v", err)
	}
}
