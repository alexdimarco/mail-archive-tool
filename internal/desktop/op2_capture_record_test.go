package desktop

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-646, R18
// A scheduled capture that refuses before app.RunGraph (not configured) with
// the archive location known must record a FAILED run, so status/health read
// RED instead of staying GREEN until the run is twice the period late (OP-2,
// R18). With no archive location there is nowhere to record.
func TestHeadlessCaptureRecordsARefusalAsAFailedRun(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "archive")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		ConfigPath:   filepath.Join(dir, "graph.json"), // absent: not configured
		SettingsPath: filepath.Join(dir, "settings.json"),
		Out:          out,
	}
	err := HeadlessCapture(cfg, io.Discard)
	if err == nil {
		t.Fatal("HeadlessCapture with no config returned nil, want a refusal")
	}
	lr, st, rerr := state.ReadLastRun(out)
	if rerr != nil || st != state.LastRunPresent {
		t.Fatalf("no last-run record after a refusal (state=%v err=%v) — status would stay GREEN", st, rerr)
	}
	if lr.Status != state.RunFailed {
		t.Errorf("recorded run status %q, want %q", lr.Status, state.RunFailed)
	}
	if !strings.Contains(strings.ToLower(lr.Error), "configured") && !strings.Contains(strings.ToLower(lr.Error), "setup") {
		t.Errorf("recorded failure error %q does not name the refusal", lr.Error)
	}

	// Negative twin: no archive location → nothing to record, just the error.
	noout := Config{ConfigPath: filepath.Join(dir, "graph.json"), SettingsPath: cfg.SettingsPath, Out: ""}
	if err := HeadlessCapture(noout, io.Discard); err == nil {
		t.Error("HeadlessCapture with no out returned nil, want a refusal")
	}
}
