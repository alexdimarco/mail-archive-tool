package app

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-51, R6
// DiscoverInputs expands a directory to the .pst/.ost data files inside it and
// de-duplicates, but treats a mail-store directory (e.g. a maildir) as one source
// rather than expanding it. This is the discovery the CLI -auto flag and the GUI
// "Auto-detect" step both build on.
func TestDiscoverInputs(t *testing.T) {
	// A directory of Outlook data files plus an unrelated file.
	dataDir := t.TempDir()
	for _, name := range []string{"a.pst", "b.ost", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := DiscoverInputs([]string{dataDir}, false)
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, got, "data files discovered under a directory")
	if len(got) != 2 {
		t.Fatalf("discovered %d files, want 2 (.pst + .ost only): %v", len(got), got)
	}
	for _, f := range got {
		switch ext := filepath.Ext(f); ext {
		case ".pst", ".ost":
		default:
			t.Errorf("discovered a non-data file: %q", f)
		}
	}

	// A maildir store directory is a single source, not expanded into files.
	store := t.TempDir()
	if err := os.MkdirAll(filepath.Join(store, "cur"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = DiscoverInputs([]string{store}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != store {
		t.Errorf("mail-store dir = %v, want [%s] (must not be expanded)", got, store)
	}

	// The same file listed twice is de-duplicated.
	got, err = DiscoverInputs([]string{filepath.Join(dataDir, "a.pst"), filepath.Join(dataDir, "a.pst")}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("duplicate input not de-duplicated: %v", got)
	}
}
