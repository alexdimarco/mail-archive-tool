package desktop

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// backupFake is an OS scheduler that holds weekly-backup tasks by the archive location each is named for.
type backupFake struct {
	tasks   map[string]bool
	removed []string
}

func (b *backupFake) installedFor(out string) bool { return b.tasks[out] }
func (b *backupFake) removeFor(out string) error {
	b.removed = append(b.removed, out)
	delete(b.tasks, out)
	return nil
}

// useBackupFake makes the dashboard ask the fake scheduler about each location, as the real one does (the
// task is named for the archive location, schedule.DefaultNameFor), and not a fixed yes or no.
func useBackupFake(e *wsEnv, b *backupFake) {
	e.cfg.BackupInstalled, e.cfg.RemoveBackup = nil, nil
	e.cfg.BackupInstalledFor, e.cfg.RemoveBackupFor = b.installedFor, b.removeFor
	e.d.cfg = e.cfg
}

func weeklyBackupCard(e *wsEnv) string {
	for _, c := range e.d.cfg.cards() {
		if c.Label == "Weekly backup" {
			return c.Value
		}
	}
	return ""
}

// covers: MA-523, R12, S41
// The Settings-save guard keeps guarding after "Save and keep the backup" (E4-AI6): the weekly task is
// named for the archive location it was installed for, and capture reads the location from the settings at
// run time, so after a keep the task named for A still runs and archives into B. The guard, the Weekly
// backup card, the state the page reads and Remove looked for the task named for the NEW location, found
// none, and said "Not installed": the next save (B to C) asked nothing, a sign-in as another account was
// not asked about, and Remove could not remove the task. The settings now remember the location the
// installed task is named for, and every one of those looks there too.
func TestSettingsGuardKeepsGuardingAfterSaveAndKeepTheBackup(t *testing.T) {
	e := newWsEnv(t)
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	fake := &backupFake{tasks: map[string]bool{a: true}}
	useBackupFake(e, fake)
	if err := UpdateSettings(e.cfg.SettingsPath, "test", func(s *Settings) { s.Out = a }); err != nil {
		t.Fatal(err)
	}
	save := func(out, backup string) map[string]any {
		_, body := e.post("/api/settings", map[string]any{"out": out, "backup": backup})
		return body
	}
	if got := weeklyBackupCard(e); got != "Installed" {
		t.Fatalf("the positive twin, the card before any change: %q", got)
	}

	if body := save(b, ""); body["confirm"] != true {
		t.Fatalf("the first retarget was not asked about: %v", body)
	}
	if body := save(b, "keep"); body["ok"] != true || LoadSettings(e.cfg.SettingsPath).Out != b {
		t.Fatalf("Save and keep the backup: %v", body)
	}
	if got := weeklyBackupCard(e); got != "Installed" {
		t.Errorf("after Save and keep the backup the Weekly backup card says %q: the task named for the old location still runs", got)
	}
	if !e.d.backupInstalled() {
		t.Error("after Save and keep the backup the dashboard no longer knows a weekly backup is installed: a sign-in as another account is not asked about")
	}
	body := save(c, "")
	if body["confirm"] != true || !strings.Contains(body["error"].(string), c) {
		t.Errorf("a second retarget (B to C) with the backup still installed: %v, want it asked about, naming %s", body, c)
	}
	if got := LoadSettings(e.cfg.SettingsPath).Out; got != b {
		t.Errorf("the second retarget was saved without an answer: location %q", got)
	}
	_, st := e.post("/api/schedule-state", nil)
	if st["state"] != "installed" {
		t.Errorf("the state the page reads: %v", st)
	}

	// Remove removes the task that is there, wherever it is named for
	if _, out := e.post("/api/schedule-remove", nil); out["ok"] != true || out["removed"] != true {
		t.Errorf("Remove: %v", out)
	}
	if fake.tasks[a] || len(fake.removed) != 1 || fake.removed[0] != a {
		t.Errorf("Remove left the task named for %s (tasks %v, removed %v)", a, fake.tasks, fake.removed)
	}
	if got := weeklyBackupCard(e); got == "Installed" || e.d.backupInstalled() {
		t.Errorf("after Remove the card says %q, installed %v", got, e.d.backupInstalled())
	}
	if body := save(c, ""); body["ok"] != true {
		t.Errorf("a save with no backup installed asks nothing: %v", body)
	}
}

// covers: MA-523, R12, S41
// Two halves of E4-AI6 that no test held (re-verification): a plain Save that does not change the location
// leaves the location the installed task is named for as it is (a Save of another checkbox after "Save and
// keep the backup" must not make the guard blind again), and installing the weekly backup after a keep
// removes the task that kept the old location's name (it would run beside the new one and archive into the
// same folder). The install is the code's stand-in: no OS scheduler is touched.
func TestSettingsKeepTheBackupNameAcrossASaveAndAnInstallRemovesTheOldTask(t *testing.T) {
	e := newWsEnv(t)
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	fake := &backupFake{tasks: map[string]bool{a: true}}
	useBackupFake(e, fake)
	var installed []string
	e.cfg.InstallBackup = func(spec schedule.Spec) error {
		installed = append(installed, spec.Out)
		fake.tasks[spec.Out] = true
		return nil
	}
	e.d.cfg = e.cfg
	if err := UpdateSettings(e.cfg.SettingsPath, "test", func(s *Settings) { s.Out = a }); err != nil {
		t.Fatal(err)
	}
	save := func(out, backup string, keepRaw bool) map[string]any {
		_, body := e.post("/api/settings", map[string]any{"out": out, "backup": backup, "keepRaw": keepRaw})
		return body
	}
	if body := save(b, "keep", false); body["ok"] != true || LoadSettings(e.cfg.SettingsPath).BackupFor != a {
		t.Fatalf("Save and keep the backup: %v, backupFor %q", body, LoadSettings(e.cfg.SettingsPath).BackupFor)
	}

	// a plain Save of another setting, the location as it is
	if body := save(b, "", true); body["ok"] != true || body["confirm"] == true {
		t.Fatalf("a save that leaves the location as it is: %v", body)
	}
	if got := LoadSettings(e.cfg.SettingsPath); got.BackupFor != a || !got.KeepRaw {
		t.Errorf("after a plain Save the settings name the task for %q (keepRaw %v), want it still named for %s", got.BackupFor, got.KeepRaw, a)
	}
	if got := weeklyBackupCard(e); got != "Installed" || !e.d.backupInstalled() {
		t.Errorf("after a plain Save the card says %q: the guard is blind to the task named for %s", got, a)
	}
	if body := save(c, "", true); body["confirm"] != true {
		t.Errorf("a retarget after a plain Save was not asked about: %v", body)
	}

	// install: the task is named for the location now, and the one that kept the old name is removed
	if _, out := e.post("/api/schedule-install", map[string]any{}); out["ok"] != true {
		t.Fatalf("Install: %v", out)
	}
	if len(installed) != 1 || installed[0] != b {
		t.Fatalf("the install was handed %v, want a task for %s", installed, b)
	}
	if fake.tasks[a] || !fake.tasks[b] || len(fake.removed) != 1 || fake.removed[0] != a {
		t.Errorf("after Install the tasks are %v (removed %v): the task named for %s runs beside the new one", fake.tasks, fake.removed, a)
	}
	if got := LoadSettings(e.cfg.SettingsPath).BackupFor; got != "" {
		t.Errorf("after Install the settings still name the task for %q", got)
	}
}

// covers: MA-523, R12, S41
// "Save and remove the weekly backup" that cannot save tells the owner the backup is gone (E4-AI7): it
// removes the backup first (a failed removal saves nothing) and saves second, and when the settings file
// could not be read at that moment the reply said only that nothing was saved, so the owner took it for
// Cancel and had lost the weekly backup. The words say what was and was not done. And the first ask names
// the folder the save will leave as the archive location: an empty field means the launch location, and a
// save that leaves the location as it is asks nothing.
func TestSaveAndRemoveTheBackupSaysSoWhenTheSaveFailsAfterIt(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("an unreadable settings file is modelled with permission bits, for a user who is not root")
	}
	e := newWsEnv(t)
	a, b := t.TempDir(), t.TempDir()
	fake := &backupFake{tasks: map[string]bool{a: true}}
	useBackupFake(e, fake)
	if err := UpdateSettings(e.cfg.SettingsPath, "test", func(s *Settings) { s.Out = a }); err != nil {
		t.Fatal(err)
	}
	save := func(out, backup string) map[string]any {
		_, body := e.post("/api/settings", map[string]any{"out": out, "backup": backup})
		return body
	}

	// the empty field: the launch location is the archive after the save
	if body := save("", ""); body["confirm"] != true || !strings.Contains(body["error"].(string), e.out) || strings.Contains(body["error"].(string), "into .") {
		t.Errorf("an empty location with the backup installed: %v, want the ask to name the launch location %s", body, e.out)
	}
	if err := UpdateSettings(e.cfg.SettingsPath, "test", func(s *Settings) { s.Out = "" }); err != nil {
		t.Fatal(err)
	}
	fake.tasks = map[string]bool{e.out: true}
	if body := save("", ""); body["ok"] != true || body["confirm"] == true {
		t.Errorf("a save that leaves the location as it is (empty, the launch location in use) asked: %v", body)
	}
	if err := UpdateSettings(e.cfg.SettingsPath, "test", func(s *Settings) { s.Out = a }); err != nil {
		t.Fatal(err)
	}
	fake.tasks = map[string]bool{a: true}

	// the settings file cannot be read while the backup is being removed
	if err := os.Chmod(e.cfg.SettingsPath, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(e.cfg.SettingsPath, 0o600) })
	// the guard reads the settings too, and an unreadable file reads as none: the archive is the launch location
	fake.tasks = map[string]bool{e.out: true}
	body := save(b, "remove")
	if body["ok"] != false {
		t.Fatalf("a save that could not be written: %v", body)
	}
	msg, _ := body["error"].(string)
	if len(fake.removed) != 1 {
		t.Fatalf("the backup was removed %d times: %v", len(fake.removed), fake.removed)
	}
	if !strings.Contains(msg, "weekly backup was removed") || !strings.Contains(msg, "settings were not saved") {
		t.Errorf("the reply %q does not say the weekly backup was removed and the settings were not saved", msg)
	}
}
