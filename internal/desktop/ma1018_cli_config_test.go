package desktop

import (
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/graphconfig"
)

// covers: MA-1018, R36
// The command line's delete reaches a Desktop started with -config (the 1.5.3 friction review's
// B19, the Desktop's side): NewCommandLineDeleteAt reads the list of archives, the settings and
// deletions.log beside the config file it is given — the very files that Desktop keeps there —
// so a row that Desktop listed is found, and a delete is logged where that Desktop reads it.
// Positive twin first: NewCommandLineDelete still reads the files beside the default config.
func TestTheCommandLineDeleteReadsTheFilesBesideTheConfigItIsGiven(t *testing.T) {
	def, err := graphconfig.DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCommandLineDelete(nil, nil, nil)
	if err != nil || c.ListPath() != filepath.Join(filepath.Dir(def), archindex.Name) {
		t.Fatalf("the positive twin: the default reads %q (%v), want the list beside %s", c.ListPath(), err, def)
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "other-config.json")
	desk := Config{ConfigPath: cfgPath, SettingsPath: settingsPathFor(cfgPath), IndexPath: indexPathFor(cfgPath)} // as launch.go builds it
	a := filepath.Join(t.TempDir(), "Listed")
	buildArchiveAt(t, a)
	now := time.Now()
	if err := archindex.WriteFile(desk.IndexPath, archindex.Index{Version: archindex.Version, Entries: []archindex.Entry{{Path: a, AddedAt: now, LastSeenAt: now}}}); err != nil {
		t.Fatal(err)
	}

	at := NewCommandLineDeleteAt(cfgPath, nil, nil, nil)
	if at.ListPath() != desk.IndexPath || at.cfg.SettingsPath != desk.SettingsPath {
		t.Errorf("the list and settings read are %q and %q, want %q and %q", at.ListPath(), at.cfg.SettingsPath, desk.IndexPath, desk.SettingsPath)
	}
	if got, want := at.PlanDeps().Log, desk.deletionsLogPath(); got != want || got != filepath.Join(dir, archdelete.LogName) {
		t.Errorf("deletions.log is %q, want %q beside the config", got, want)
	}
	if row, ok, err := at.Row(a); err != nil || !ok || row != a {
		t.Errorf("the row the -config Desktop listed is not found: %q %v %v", row, ok, err)
	}
	if _, ok, _ := c.Row(a); ok {
		t.Error("the default list holds the -config Desktop's row too, so this test proves nothing")
	}
}
