package uninstall

import (
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/archdelete"
)

// covers: MA-965, R36
// Uninstall removes the deletion log with the other settings files
// (docs/design-archive-delete.md §5.4, §13 slice 5): ConfigTargets names
// deletions.log — the name the delete appends to (archdelete.LogName) — beside the
// settings file it already removed (the positive twin).
func TestUninstallRemovesTheDeletionLog(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "desktop-settings.json"), "{}")
	writeFile(t, filepath.Join(dir, archdelete.LogName), `{"event":"started"}`+"\n")
	targets, err := ConfigTargets(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range targets {
		got[filepath.Base(p)] = true
	}
	if !got["desktop-settings.json"] {
		t.Fatalf("positive twin: the settings file is a target: %q", targets)
	}
	if !got[archdelete.LogName] {
		t.Fatalf("deletions.log must be among uninstall's targets: %q", targets)
	}
}
