package app

import (
	"context"
	"errors"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-1303, R45
// Ctrl-C while the terminal asks the Gmail question stops the run cleanly (the B3 adversarial
// pass's A4): the question ends as no answer, nothing is switched, and the run ends as an
// interrupted run does, recorded cancelled, its archive's lock released. Positive twin first: a
// question answered Not now with no interrupt finishes the backup ok.
func TestCtrlCDuringTheGmailQuestionStopsTheRunAsInterrupted(t *testing.T) {
	store, out := existingGmailArchive(t)
	res, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Mode: export.Incremental,
		AskGmail: func(string) bool { return false }}, discard(), nil)
	if lr, _, _ := state.ReadLastRun(out); err != nil || lr.Status != state.RunOK || len(res.Gmail.Switched) != 0 {
		t.Fatalf("positive twin: %v, status %q, switched %v", err, lr.Status, res.Gmail.Switched)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	asked := 0
	_, err = Run(ctx, Options{Inputs: []string{store}, Out: out, Mode: export.Incremental,
		AskGmail: func(string) bool { asked++; cancel(); return false }}, discard(), nil)
	if asked != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("Ctrl-C during the question: asked %d, run error %v", asked, err)
	}
	if lr, _, _ := state.ReadLastRun(out); lr.Status != state.RunCancelled {
		t.Fatalf("the interrupted run is recorded %q, want %q", lr.Status, state.RunCancelled)
	}
	if v, on := manifestOnDisk(t, out); v != 6 || len(on) != 0 {
		t.Fatalf("the interrupted run switched the archive: version %d, on %v", v, on)
	}
	// The lock is free: another run can take it.
	if _, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Mode: export.Incremental, Unattended: true}, discard(), nil); err != nil {
		t.Fatalf("a run after the interrupted one: %v", err)
	}
}
