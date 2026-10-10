package app

import (
	"reflect"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-375, R12, R17, S45
// The warnings a caller found with a run's setup (a credential inside the
// archive, P10) are in the last-run record from the moment the run is recorded
// running, and stay in its final record, where `status` reads them; a run with
// none records none, so a fixed setup clears the WARN.
func TestRunRecordsSetupWarnings(t *testing.T) {
	out := t.TempDir()
	warnings := []state.RunWarning{{Code: "credential_in_archive", Message: "the token cache " + out + "/graph.token is inside the archive folder " + out}}
	opts := Options{Out: out, Warnings: warnings}

	lr := beginRun(opts)
	running, st, err := state.ReadLastRun(out)
	if err != nil || st != state.LastRunPresent || running.Status != state.RunRunning {
		t.Fatalf("running record: %+v state=%v err=%v", running, st, err)
	}
	if !reflect.DeepEqual(running.Warnings, warnings) {
		t.Errorf("the running record carries warnings %+v, want %+v", running.Warnings, warnings)
	}

	var res Result
	var runErr error
	recordRun(opts, lr, &res, &runErr)
	final, _, _ := state.ReadLastRun(out)
	if final.Status != state.RunOK || !reflect.DeepEqual(final.Warnings, warnings) {
		t.Errorf("the final record is %s with warnings %+v, want ok with %+v", final.Status, final.Warnings, warnings)
	}

	beginRun(Options{Out: out})
	if clean, _, _ := state.ReadLastRun(out); clean.Warnings != nil {
		t.Errorf("a run with no warnings recorded %+v", clean.Warnings)
	}
}
