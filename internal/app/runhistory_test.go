package app

import (
	"errors"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-701
// recordRun adds every finalized run to the archive's rolling history (the integration
// that makes the Recent backups card work), with the status and error the run ended
// with — not just the overwritten last-run slot.
func TestMA701_RunRecordedInHistory(t *testing.T) {
	var got []state.RunHistoryEntry
	oldAppend := appendRunHistory
	oldWrite := writeLastRun
	defer func() { appendRunHistory, writeLastRun = oldAppend, oldWrite }()
	appendRunHistory = func(out string, e state.RunHistoryEntry) error { got = append(got, e); return nil }
	writeLastRun = func(out string, r state.LastRun) error { return nil } // keep the test off disk

	opts := Options{Out: t.TempDir()}
	base := state.LastRun{Started: time.Now().Add(-time.Second), Mode: "incremental"}

	// A successful run is recorded as ok.
	var runErr error
	recordRun(opts, base, &Result{}, &runErr, nil)
	if len(got) != 1 || got[0].Status != state.RunOK {
		t.Fatalf("a successful run must be in history as ok, got %+v", got)
	}

	// A failed run is recorded as failed, with its reason.
	got = nil
	runErr = errors.New("the backup drive was not mounted")
	recordRun(opts, base, &Result{}, &runErr, nil)
	if len(got) != 1 || got[0].Status != state.RunFailed || got[0].Error == "" {
		t.Fatalf("a failed run must be in history with its error, got %+v", got)
	}
}
