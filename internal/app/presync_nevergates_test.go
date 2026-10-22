package app

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-560
// The pre-sync never gates the archive (invariant R32, design §4.9.1): a hook
// that returns a warning, and one that PANICS, both leave the archive run intact
// — Run reads the sources, returns nil with status ok, and records the warning
// (a recovered panic as presync_internal_error).
func TestBeforeReadNeverGatesArchive(t *testing.T) {
	run := func(t *testing.T, hook func(context.Context, []string) BeforeReadResult) (Result, error, state.LastRun) {
		t.Helper()
		src := filepath.Join(tmpDir(t), "Inbox")
		maildirMessage(t, src, "")
		out := tmpDir(t)
		opts := Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, BeforeRead: hook}
		res, err := Run(context.Background(), opts, log.New(io.Discard, "", 0), nil)
		lr, _, _ := state.ReadLastRun(out)
		return res, err, lr
	}
	hasWarning := func(lr state.LastRun, code string) bool {
		for _, w := range lr.Warnings {
			if w.Code == code {
				return true
			}
		}
		return false
	}

	t.Run("a warning-returning hook does not gate the archive", func(t *testing.T) {
		res, err, lr := run(t, func(ctx context.Context, files []string) BeforeReadResult {
			return BeforeReadResult{Warnings: []state.RunWarning{{Code: "presync_cannot_start", Message: "x"}}}
		})
		if err != nil {
			t.Fatalf("the warning must not fail the run: %v", err)
		}
		if res.Fillable != 1 {
			t.Errorf("the archive did not run (fillable=%d, want 1)", res.Fillable)
		}
		if lr.Status != state.RunOK {
			t.Errorf("status = %q, want ok", lr.Status)
		}
		if !hasWarning(lr, "presync_cannot_start") {
			t.Errorf("the pre-sync warning was not recorded: %+v", lr.Warnings)
		}
	})

	t.Run("a panicking hook is recovered and the archive still runs", func(t *testing.T) {
		res, err, lr := run(t, func(ctx context.Context, files []string) BeforeReadResult {
			panic("pre-sync boom")
		})
		if err != nil {
			t.Fatalf("a panicking hook must not fail the run: %v", err)
		}
		if res.Fillable != 1 {
			t.Errorf("the archive did not run after a panic (fillable=%d, want 1)", res.Fillable)
		}
		if lr.Status != state.RunOK {
			t.Errorf("status = %q, want ok", lr.Status)
		}
		if !hasWarning(lr, "presync_internal_error") {
			t.Errorf("a recovered panic was not recorded as presync_internal_error: %+v", lr.Warnings)
		}
	})
}
