package uninstall

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
)

// covers: MA-1292, R37
// Uninstall removes the Gmail answers this computer keeps (<key>.gmail-answer beside the job
// files) with the other settings files, by the suffix gmailanswer writes (the two kept in step by
// behaviour: the name comes from gmailanswer.Path). Positive twin first: the settings file is a
// target; an archive marker beside it never is.
func TestUninstallRemovesTheGmailAnswersKeptHere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	// Every answer names its archive (the B3 adversarial pass's A1); none is read here.
	if err := gmailanswer.KeepHere(filepath.Join(home, "archive"), gmailanswer.Answer{Archive: state.NewArchiveID(), NotNow: true}); err != nil {
		t.Fatal(err)
	}
	p, err := gmailanswer.Path(filepath.Join(home, "archive"))
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
		t.Fatalf("uninstall leaves the Gmail answer %s behind: targets %v", filepath.Base(p), targets)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}
