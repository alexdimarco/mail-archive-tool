package export_test

import (
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

const fixture = "../../testdata/support.pst"

func runExport(t *testing.T, out string, mode export.Mode, manifest *state.Manifest) export.Stats {
	t.Helper()
	r, err := source.Open(fixture)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer r.Close()

	exp := &export.Exporter{
		OutDir:   out,
		Manifest: manifest,
		Mode:     mode,
		Log:      log.New(io.Discard, "", 0),
	}
	err = r.Walk(func(folderPath []string, m *model.Message) error {
		_, err := exp.Export(r.StoreName(), folderPath, m)
		return err
	})
	if err != nil {
		t.Fatalf("walk/export: %v", err)
	}
	return exp.Stats
}

func countFiles(t *testing.T, root, suffix string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, suffix) {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// covers: MA-22
func TestExportIncrementalLifecycle(t *testing.T) {
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	out := t.TempDir()
	mpath := filepath.Join(out, "manifest.json")

	// First run writes everything.
	m1, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	s1 := runExport(t, out, export.Incremental, m1)
	if s1.Exported == 0 {
		t.Fatal("first run exported nothing")
	}
	if err := m1.Save(); err != nil {
		t.Fatal(err)
	}

	if got := countFiles(t, out, ".html"); got != s1.Exported {
		t.Errorf("html files on disk = %d, want %d", got, s1.Exported)
	}

	// Second incremental run: everything already seen, nothing new.
	m2, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	s2 := runExport(t, out, export.Incremental, m2)
	if s2.Exported != 0 {
		t.Errorf("second incremental run exported %d, want 0", s2.Exported)
	}
	if s2.SkippedManifest != s1.Exported {
		t.Errorf("second run skipped %d, want %d", s2.SkippedManifest, s1.Exported)
	}

	// Full run re-exports everything regardless of the manifest.
	m3, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	s3 := runExport(t, out, export.Full, m3)
	if s3.Exported != s1.Exported {
		t.Errorf("full run exported %d, want %d", s3.Exported, s1.Exported)
	}
}
