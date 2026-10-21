package uninstall

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-671
// Uninstall never deletes an archive (R36): ConfigTargets returns only an explicit
// allow-list of regular config files, NEVER an archive marker (.mailarchive-*, which a
// *.json glob would match since Go's '*' matches a leading dot) or a directory; and it
// REFUSES the whole cleanup when the config dir is, contains, or sits inside an archive
// (U-C1/U-C2).
func TestMA671_ConfigTargetsNeverTouchesAnArchive(t *testing.T) {
	// A clean config dir: the allow-list is returned; a stray archive marker and a
	// subdirectory are excluded.
	cfg := t.TempDir()
	allow := []string{"desktop-settings.json", "graph-config.json", "last-archive.txt", "known-archives.json", "desktop-capture.log", "backup.json", "weekly.jobfail.log"}
	for _, n := range allow {
		writeFile(t, filepath.Join(cfg, n), "x")
	}
	writeFile(t, filepath.Join(cfg, ".mailarchive-paused.json"), "x") // a stray archive marker
	if err := os.Mkdir(filepath.Join(cfg, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	targets, err := ConfigTargets(cfg, nil)
	if err != nil {
		t.Fatalf("a clean config dir must not refuse: %v", err)
	}
	got := map[string]bool{}
	for _, p := range targets {
		got[filepath.Base(p)] = true
	}
	for _, want := range allow {
		if !got[want] {
			t.Errorf("the allow-list is missing %q", want)
		}
	}
	if got[".mailarchive-paused.json"] {
		t.Fatal("an archive marker must NEVER be a delete target (U-C2; Go *.json matches a leading dot)")
	}
	if got["sub"] {
		t.Fatal("a directory must never be a delete target")
	}

	// (a) the config dir itself holds a manifest -> refuse, delete nothing.
	cfgIsArchive := t.TempDir()
	writeFile(t, filepath.Join(cfgIsArchive, manifestName), `{"version":6,"stores":{}}`)
	if _, err := ConfigTargets(cfgIsArchive, nil); !errors.Is(err, ErrOverlap) {
		t.Fatalf("a config dir holding a manifest must refuse with ErrOverlap, got %v", err)
	}

	// (b) a known archive lives INSIDE the config dir -> refuse.
	cfgParent := t.TempDir()
	archInside := filepath.Join(cfgParent, "inside-archive")
	if err := os.MkdirAll(archInside, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(archInside, manifestName), `{"version":6,"stores":{}}`)
	if _, err := ConfigTargets(cfgParent, []string{archInside}); !errors.Is(err, ErrOverlap) {
		t.Fatalf("a config dir containing an archive must refuse, got %v", err)
	}

	// (c) the config dir lives INSIDE an archive -> refuse.
	archRoot := t.TempDir()
	writeFile(t, filepath.Join(archRoot, manifestName), `{"version":6,"stores":{}}`)
	cfgInside := filepath.Join(archRoot, "cfg")
	if err := os.MkdirAll(cfgInside, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfigTargets(cfgInside, []string{archRoot}); !errors.Is(err, ErrOverlap) {
		t.Fatalf("a config dir inside an archive must refuse, got %v", err)
	}
}
