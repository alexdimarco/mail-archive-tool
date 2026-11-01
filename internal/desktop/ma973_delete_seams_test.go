package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-973, R36
// Deleting the current archive from the Desktop reaches this computer through its real seams
// (design-archive-delete §5.1, §5.3 steps 2-4, 9; C4, C6): the scheduler stops the archive's
// own entry, which its job file names, and the current archive's capture registered under
// another folder's name (§5.1 (c)); step 3 removes the job file and the alert files beside the
// Desktop's settings; step 4 clears Out, BackupFor and last-archive.txt; step 9 removes the
// row from the Desktop's own list. Positive twin first: an archive with no schedule and no
// pointer is deleted and nothing else is stopped or cleared.
func TestTheDesktopsDeleteReachesItsSchedulesSettingsAndList(t *testing.T) {
	e := newDeleteEnv(t)
	other := e.plainArchive("Other")
	keep := e.plainArchive("Kept")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: keep}); err != nil {
		t.Fatal(err)
	}
	if r := e.planAndDelete(other); r["ok"] != true {
		t.Fatalf("the positive twin: %v", r)
	}
	if got := e.sched.removedNames(); len(got) != 0 {
		t.Errorf("the twin stopped %v", got)
	}
	if s := LoadSettings(e.cfg.SettingsPath); s.Out != keep {
		t.Errorf("the twin changed Out to %q", s.Out)
	}

	// The current archive, with its run-job, its capture registered under X's name, its alert
	// files and the wizard's note of it.
	dir := e.plainArchive("Current")
	x := t.TempDir()
	jobFile := plantRunJob(t, dir)
	e.sched.installed[schedule.DefaultNameFor(dir)] = true
	e.sched.installed[schedule.DefaultNameFor(x)] = true
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: dir, BackupFor: x, WeeklyTime: "04:00"}); err != nil {
		t.Fatal(err)
	}
	lastFile, _ := job.LastArchiveFile()
	if err := os.WriteFile(lastFile, []byte(dir+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := schedule.DefaultNameFor(dir)
	streak := filepath.Join(filepath.Dir(e.cfg.SettingsPath), key+".alert-streak")
	syncAlert := filepath.Join(filepath.Dir(e.cfg.SettingsPath), key+".sync-alert")
	for _, f := range []string{streak, syncAlert} {
		os.WriteFile(f, []byte("{}"), 0o600)
	}

	p := e.plan(dir)
	if p["ok"] != true {
		t.Fatalf("the plan: %v", p)
	}
	stops, _ := p["schedules"].([]any)
	if len(stops) < 2 {
		t.Errorf("the plan names %v, want the archive's entry and the capture registered under X", stops)
	}
	r := e.deleteWith(p["token"].(string), "Current", false)
	if r["ok"] != true || r["status"] != "complete" {
		t.Fatalf("the delete: %v", r)
	}
	got := e.sched.removedNames()
	sort.Strings(got)
	want := []string{schedule.DefaultNameFor(dir), schedule.DefaultNameFor(x)}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stopped %v, want %v", got, want)
	}
	for _, f := range []string{jobFile, streak, syncAlert} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s is still there: %v", f, err)
		}
	}
	s := LoadSettings(e.cfg.SettingsPath)
	if s.Out != "" || s.BackupFor != "" || s.WeeklyTime != "04:00" {
		t.Errorf("settings after the delete: Out %q, BackupFor %q, WeeklyTime %q", s.Out, s.BackupFor, s.WeeklyTime)
	}
	if got := job.ReadLastArchive(lastFile); got != "" {
		t.Errorf("last-archive.txt still names %q", got)
	}
	if e.listed(dir) {
		t.Error("the row is still listed")
	}
}

// covers: MA-973, R36
// The Desktop's own settings folder is protected by identity (§7 protected row; C15): a
// Desktop started with -config inside a folder keeps its settings there, and Delete of that
// folder is refused, nothing changed. Positive twin first: the same archive with the settings
// elsewhere is planned.
func TestTheDesktopsSettingsFolderIsProtected(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Holds Settings")
	if p := e.plan(dir); p["ok"] != true {
		t.Fatalf("the positive twin: %v", p)
	}
	inside := filepath.Join(dir, "settings")
	os.MkdirAll(inside, 0o700)
	e2 := newDeleteEnv(t, func(w *wsEnv) { w.cfg.ConfigPath = filepath.Join(inside, "graph-config.json") })
	e2.list(dir)
	before := treeState(t, dir)
	p := e2.plan(dir)
	assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK), assure.Names("MailArchive will not delete this folder", inside),
		assure.NoSideEffect(func() bool { return reflect.DeepEqual(treeState(t, dir), before) && p["token"] == nil }))
}
