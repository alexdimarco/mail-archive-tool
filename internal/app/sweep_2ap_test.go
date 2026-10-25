package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
)

// stale writes name under dir with a modification time an hour ago: something
// a crashed earlier run left behind.
func stale(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("left by a crash"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	return p
}

// covers: MA-721, MA-100, R5
func TestACopyFirstRunSweepsAStaleSnapshotAndLeavesNone(t *testing.T) {
	dir := tmpDir(t)
	src := writeData(t, dir, "support.pst", pstFixtureData(t))
	out := tmpDir(t)
	left := stale(t, out, export.SnapshotPrefix+"crashed.pst")

	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, CopyFirst: true}, discard(), nil); err != nil {
		t.Fatalf("a -copy-first run over a healthy PST: %v", err)
	}
	assure.Reached(t, loadArchive(t, out).All(), "archived records (the snapshot keeps its extension, so the PST reader still reads it)")
	if _, err := os.Stat(left); err == nil {
		t.Error("the snapshot a crashed run left behind (a whole mailbox copy) was not swept")
	}
	if snaps, _ := filepath.Glob(filepath.Join(out, export.SnapshotPrefix+"*")); len(snaps) != 0 {
		t.Errorf("snapshots left after the run: %v", snaps)
	}
}

// covers: MA-721, R5
func TestAGraphCaptureSweepsStaleTempsButKeepsZips(t *testing.T) {
	s := sweepServer(false, []string{"A1"}, nil, nil)
	defer s.Close()
	out := tmpDir(t)
	tmp := stale(t, out, ".mailarchive-crashed.tmp")
	manifestTmp := stale(t, out, ".mailarchive-manifest-crashed.tmp")
	zip := stale(t, out, filepath.Join("someone", "Inbox", "2026-01-01_0000_kept_00000000-attachments.zip"))

	if _, err := sweepRun(isoOptions(s), out, export.Incremental); err != nil {
		t.Fatalf("capture: %v", err)
	}
	for _, p := range []string{tmp, manifestTmp} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("a capture left a crashed run's temp in place: %s", p)
		}
	}
	if _, err := os.Stat(zip); err != nil {
		t.Errorf("a capture removed an attachment zip with no page beside it (only local runs sweep those): %v", err)
	}
}
