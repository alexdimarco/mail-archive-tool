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
	"mail-archive-tool/internal/state"
)

// covers: MA-76, R18, S29
// Every run that begins is recorded: a successful run leaves status ok with
// its counts; a run that fails after starting leaves status failed with the
// error; a cancelled run leaves cancelled. The record is written as "running"
// first, so a run that never finishes stays visible as such. A finalized-failed
// run also drops a human-facing BACKUP-NEEDS-ATTENTION.txt sidecar naming the
// reason and the status remedy; a later successful run clears it (P7).
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
	// A successful run leaves no attention sidecar.
	if _, serr := os.Stat(filepath.Join(out, state.AttentionName)); !os.IsNotExist(serr) {
		t.Errorf("a successful run left a %s sidecar", state.AttentionName)
	}

	// A run that starts and then FAILS after the lock: a corrupt manifest is
	// loaded once the run is under way, so it finalizes failed (a missing -input
	// path now refuses before the run starts, creating nothing — friction #2).
	out2 := tmpDir(t)
	if err := os.WriteFile(filepath.Join(out2, ".mailarchive-manifest.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Run(context.Background(), Options{Inputs: []string{src}, Out: out2, Index: true}, logger, nil)
	if err == nil {
		t.Fatal("expected a failure")
	}
	lr, _, _ = state.ReadLastRun(out2)
	if lr.Status != state.RunFailed || lr.Error == "" {
		t.Errorf("failed record wrong: %+v", lr)
	}
	// The failed run drops the attention sidecar naming the reason and remedy.
	sidecar := filepath.Join(out2, state.AttentionName)
	data, serr := os.ReadFile(sidecar)
	if serr != nil {
		t.Fatalf("failed run did not write %s: %v", state.AttentionName, serr)
	}
	if !strings.Contains(string(data), "mailarchive status") || !strings.Contains(string(data), lr.Error) {
		t.Errorf("%s lacks the reason or the status remedy:\n%s", state.AttentionName, data)
	}
	// A later successful run on the same archive clears the sidecar.
	if err := os.Remove(filepath.Join(out2, ".mailarchive-manifest.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out2, Index: true}, logger, nil); err != nil {
		t.Fatal(err)
	}
	if _, serr := os.Stat(sidecar); !os.IsNotExist(serr) {
		t.Errorf("a later successful run did not remove %s", state.AttentionName)
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
