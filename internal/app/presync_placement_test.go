package app

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// covers: MA-559
// app.Run calls opts.BeforeRead exactly once, AFTER the archive lock is held and
// the running last-run record is written, and BEFORE any source is read (design
// §4.1, P4) — so R5 (one run per archive) and R18 (every run recorded) hold for
// the whole pre-sync session.
func TestBeforeReadHookPlacement(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessage(t, src, "")
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)

	called := 0
	gotFiles := 0
	lockHeld, recordWritten := false, false
	opts := Options{
		Inputs: []string{src}, Out: out, Mode: export.Incremental,
		BeforeRead: func(ctx context.Context, files []string) BeforeReadResult {
			called++
			gotFiles = len(files)
			if _, err := os.Stat(filepath.Join(out, lockfile.Name)); err == nil {
				lockHeld = true
			}
			if _, err := os.Stat(filepath.Join(out, state.LastRunName)); err == nil {
				recordWritten = true
			}
			return BeforeReadResult{}
		},
	}
	if _, err := Run(context.Background(), opts, logger, nil); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Errorf("BeforeRead called %d times, want exactly 1", called)
	}
	if gotFiles == 0 {
		t.Errorf("BeforeRead received no discovered files")
	}
	if !lockHeld {
		t.Errorf("the archive lock was not held when BeforeRead ran (placement wrong)")
	}
	if !recordWritten {
		t.Errorf("the running last-run record was not written when BeforeRead ran (placement wrong)")
	}
}
