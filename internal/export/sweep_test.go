package export

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/vault"
)

// sweepFixture is an archive holding one of each kind of leftover a crash can
// leave, each stale (older than the sweep's cutoff) unless marked fresh.
type sweepFixture struct {
	out                                         string
	staleTmp, staleManifestTmp, staleSnapshot   string
	freshTmp, freshSnapshot, orphanZip, keptZip string
	page                                        string
}

func newSweepFixture(t *testing.T) sweepFixture {
	t.Helper()
	out := t.TempDir()
	dir := filepath.Join(out, "store", "Inbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(dir, name string, stale bool) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if stale {
			old := time.Now().Add(-time.Hour)
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	return sweepFixture{
		out:              out,
		staleTmp:         write(dir, ".mailarchive-123.tmp", true),
		staleManifestTmp: write(out, ".mailarchive-manifest-456.tmp", true),
		staleSnapshot:    write(out, SnapshotPrefix+"789.pst", true), // keeps its source's extension
		freshTmp:         write(dir, ".mailarchive-999.tmp", false),
		freshSnapshot:    write(out, SnapshotPrefix+"000.ost", false), // this run's own copy
		orphanZip:        write(dir, "2026-06-01_1200_gone_ffff0000-attachments.zip", true),
		keptZip:          write(dir, "2026-06-01_1200_msg_abcd1234-attachments.zip", true),
		page:             write(dir, "2026-06-01_1200_msg_abcd1234.html", true),
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// covers: MA-721, MA-69, R5, S2
func TestSweepTempsReapsStaleTempsAndSnapshotsButNoZips(t *testing.T) {
	f := newSweepFixture(t)
	removed := SweepTemps(f.out, time.Now().Add(-time.Minute), nil)
	if removed != 3 {
		t.Errorf("removed %d, want 3: the stale temp, the stale manifest temp and the stale snapshot", removed)
	}
	for _, p := range []string{f.staleTmp, f.staleManifestTmp, f.staleSnapshot} {
		if exists(p) {
			t.Errorf("a crash leftover survived: %s", p)
		}
	}
	for _, p := range assure.Reached(t, []string{f.freshTmp, f.freshSnapshot, f.orphanZip, f.keptZip, f.page}, "files to keep") {
		if !exists(p) {
			t.Errorf("SweepTemps removed a file it must keep: %s", p)
		}
	}
}

// covers: MA-709, R37
func TestVaultTempNameIsOneTheExporterSweeps(t *testing.T) {
	out := t.TempDir()
	f, err := os.CreateTemp(out, vault.TempPattern)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(f.Name(), old, old); err != nil {
		t.Fatal(err)
	}
	if n := SweepTemps(out, time.Now(), nil); n != 1 {
		t.Fatalf("SweepTemps removed %d files, want the 1 crash-leftover vault temp", n)
	}
	if _, err := os.Stat(f.Name()); !os.IsNotExist(err) {
		t.Fatalf("the leftover vault temp %s was not reaped", f.Name())
	}
}

// covers: MA-721, MA-69, R5, S2
func TestSweepOrphanZipsReapsOnlyPagelessZips(t *testing.T) {
	f := newSweepFixture(t)
	if removed := SweepOrphanZips(f.out, nil); removed != 1 {
		t.Errorf("removed %d, want 1: the zip whose page is gone", removed)
	}
	if exists(f.orphanZip) {
		t.Error("the orphan zip survived")
	}
	for _, p := range []string{f.keptZip, f.page, f.staleTmp, f.staleSnapshot} {
		if !exists(p) {
			t.Errorf("SweepOrphanZips removed a file it must keep: %s", p)
		}
	}
}
