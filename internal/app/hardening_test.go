package app

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// covers: MA-85, R5, R13, S25
// reindex sweeps and rewrites, so it runs under the same archive lock as an
// export: while a run holds the archive, reindex refuses naming the lock.
func TestReindexRefusesLockedArchive(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	out := tmpDir(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Index: true}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	held, err := lockfile.Acquire(filepath.Join(out, lockfile.Name))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	if _, _, err := Reindex(out, log.New(io.Discard, "", 0)); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Errorf("reindex ran on a locked archive: %v", err)
	}
}

// covers: MA-100, R5, S2
// A -copy-first snapshot lands on the archive's own volume (the drive the
// operator chose and sized), never in the system temp directory.
func TestSnapshotOnArchiveVolume(t *testing.T) {
	out := tmpDir(t)
	src := filepath.Join(tmpDir(t), "data.pst")
	if err := os.WriteFile(src, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, cleanup, err := snapshot(src, out)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if filepath.Dir(snap) != out {
		t.Errorf("snapshot %s is not inside the archive %s", snap, out)
	}
	if data, _ := os.ReadFile(snap); string(data) != "bytes" {
		t.Errorf("snapshot content %q", data)
	}
}

// covers: MA-68, R1, S6
// A carriage return (or any control character) in an untrusted subject cannot
// split a verification-report row.
func TestReportRowsSurviveControlCharacters(t *testing.T) {
	out := tmpDir(t)
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.Add("k", state.Record{Path: "s/f/x.html", Folder: "F\rolder", Subject: "evil\rsubject\x1b[2J", Missing: []string{"body"}, Date: "2026-01-01T00:00:00Z"})
	path, rows := writeReport(out, m, false, log.New(io.Discard, "", 0))
	if rows != 1 {
		t.Fatalf("rows = %d", rows)
	}
	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || strings.ContainsAny(string(data), "\r\x1b") {
		t.Errorf("report rows split or control characters kept:\n%q", data)
	}
	_ = export.Incremental
}
