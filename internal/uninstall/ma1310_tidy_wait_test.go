package uninstall

import (
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/gmailanswer"
)

// covers: MA-1310, R37
// Uninstall removes this computer's record of a wait for another computer's tidy-up
// (<key>.tidy-wait, named as gmailanswer writes it) with the other settings files (the B3 UX
// walk's U2). Positive twin first: the settings file is a target and an archive marker never is.
func TestUninstallRemovesTheTidyUpWait(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	out := filepath.Join(home, "archive")
	if _, err := gmailanswer.NoteTidyWait(out, time.Now()); err != nil {
		t.Fatal(err)
	}
	p, err := gmailanswer.TidyWaitPath(out)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(p)
	writeFile(t, filepath.Join(dir, "desktop-settings.json"), "{}")
	writeFile(t, filepath.Join(dir, ".mailarchive-paused.json"), "x")
	targets, err := ConfigTargets(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tp := range targets {
		got[filepath.Base(tp)] = true
	}
	if !got["desktop-settings.json"] || got[".mailarchive-paused.json"] {
		t.Fatalf("positive twin: targets %v", targets)
	}
	if !got[filepath.Base(p)] {
		t.Fatalf("uninstall leaves the tidy-up wait %s behind: %v", filepath.Base(p), targets)
	}
}
