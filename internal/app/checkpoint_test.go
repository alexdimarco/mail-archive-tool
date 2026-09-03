package app

import (
	"context"
	"fmt"
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

// maildirMessages writes n small messages into a maildir folder.
func maildirMessages(t *testing.T, dir string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "cur"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		msg := fmt.Sprintf("From: a@example.com\r\nSubject: msg %d\r\nDate: Mon, 03 Mar 2025 09:%02d:00 +0000\r\nMessage-ID: <m%d@x>\r\n\r\nbody %d\r\n", i, i%60, i, i)
		if err := os.WriteFile(filepath.Join(dir, "cur", fmt.Sprintf("%d.eml", i)), []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// covers: MA-88, R5, S2
// Progress is checkpointed (manifest saved, index flushed) every CheckpointEvery
// messages INSIDE a store walk — not only at store boundaries — so a hard crash
// mid-store keeps the messages already written. Observed from inside the walk:
// at the 5th message the on-disk manifest already holds the first checkpoint.
func TestProgressIsCheckpointedMidWalk(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 6)
	out := tmpDir(t)
	mpath := filepath.Join(out, ".mailarchive-manifest.json")

	var onDisk int
	seen := 0
	progress := func(s export.Stats) {
		seen++
		if seen == 5 {
			m, err := state.Load(mpath)
			if err == nil {
				onDisk = m.Len()
			}
		}
	}
	opts := Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, CheckpointEvery: 2}
	if _, err := Run(context.Background(), opts, log.New(io.Discard, "", 0), progress); err != nil {
		t.Fatal(err)
	}
	if onDisk < 2 {
		t.Errorf("manifest on disk at message 5 held %d entries, want a checkpoint of at least 2", onDisk)
	}
}

// covers: MA-85, R5, R12, S25
// A run on an archive another run holds refuses up front, naming the lock and
// the holder, and writes nothing (no manifest, no index).
func TestRunRefusesLockedArchive(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 1)
	out := tmpDir(t)
	held, err := lockfile.Acquire(filepath.Join(out, lockfile.Name))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	_, err = Run(context.Background(), Options{Inputs: []string{src}, Out: out, Index: true}, log.New(io.Discard, "", 0), nil)
	if err == nil {
		t.Fatal("run on a locked archive did not refuse")
	}
	if !strings.Contains(err.Error(), lockfile.Name) || !strings.Contains(err.Error(), "in use") {
		t.Errorf("refusal must name the lock and say the archive is in use: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(out, ".mailarchive-manifest.json")); serr == nil {
		t.Error("a refused run wrote a manifest")
	}
	if _, serr := os.Stat(filepath.Join(out, "search.db")); serr == nil {
		t.Error("a refused run created an index")
	}
}
