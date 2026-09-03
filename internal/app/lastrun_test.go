package app

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-76, R18, S29
// Every run that begins is recorded: a successful run leaves status ok with
// its counts; a run that fails after starting leaves status failed with the
// error; a cancelled run leaves cancelled. The record is written as "running"
// first, so a run that never finishes stays visible as such.
func TestLastRunRecorded(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 3)
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)

	r, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true}, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	lr, st, err := state.ReadLastRun(out)
	if err != nil || st != state.LastRunPresent {
		t.Fatalf("no last-run record after a run: state=%v err=%v", st, err)
	}
	if lr.Status != state.RunOK || lr.Exported != r.Stats.Exported || lr.Finished.IsZero() || lr.PID != os.Getpid() {
		t.Errorf("ok record wrong: %+v", lr)
	}

	// A run that starts and then fails (the input vanishes after the lock).
	gone := filepath.Join(tmpDir(t), "missing-input")
	_, err = Run(context.Background(), Options{Inputs: []string{gone}, Out: out, Index: true}, logger, nil)
	if err == nil {
		t.Fatal("expected a failure")
	}
	lr, _, _ = state.ReadLastRun(out)
	if lr.Status != state.RunFailed || lr.Error == "" {
		t.Errorf("failed record wrong: %+v", lr)
	}

	// A cancelled run.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Run(ctx, Options{Inputs: []string{src}, Out: out, Mode: export.Full, Index: true}, logger, nil)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	lr, _, _ = state.ReadLastRun(out)
	if lr.Status != state.RunCancelled {
		t.Errorf("cancelled record wrong: %+v", lr)
	}

	// Absent and unreadable are distinguished.
	if _, st, _ := state.ReadLastRun(tmpDir(t)); st != state.LastRunAbsent {
		t.Errorf("absent record reported as %v", st)
	}
	bad := tmpDir(t)
	os.WriteFile(filepath.Join(bad, state.LastRunName), []byte("{torn"), 0o644)
	if _, st, err := state.ReadLastRun(bad); st != state.LastRunUnreadable || err == nil {
		t.Errorf("torn record reported as %v (%v)", st, err)
	}
}
