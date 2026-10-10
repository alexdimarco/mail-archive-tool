package app

import (
	"bytes"
	"context"
	"errors"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-399, R18, S29
// A last-run record the run could not write is logged, not dropped (g7
// friction F12): when Windows file sharing refuses the finalizing write past the
// retry budget, the run still ends as its log says, but status shows the
// "running" record as RED "never finished" (or the previous result). The log now
// carries one warning naming the record, why status may disagree, and the remedy
// (exclude the archive folder from real-time scanning). Positive twin: a run whose
// record is written logs no such warning.
func TestFailedLastRunWriteIsLogged(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	runWith := func(write func(string, state.LastRun) error) (string, error) {
		old := writeLastRun
		if write != nil {
			writeLastRun = write
		}
		defer func() { writeLastRun = old }()
		var buf bytes.Buffer
		opts := Options{Inputs: []string{src}, Out: tmpDir(t), Mode: export.Incremental, Index: true}
		_, err := Run(context.Background(), opts, log.New(&buf, "", 0), nil)
		return buf.String(), err
	}

	logs, err := runWith(nil)
	if err != nil || strings.Contains(logs, "could not record") {
		t.Fatalf("positive twin: a normal run logged %q (err %v)", logs, err)
	}

	logs, err = runWith(func(string, state.LastRun) error { return errors.New("sharing violation") })
	if err != nil {
		t.Fatalf("a refused record write must not fail the run: %v", err)
	}
	for _, want := range []string{"could not record this run", state.LastRunName, "sharing violation", "never finished", "whatever this log says"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("the log lacks %q:\n%s", want, logs)
		}
	}
	// The warning is printed from recordRun's defer, before the run's own
	// Done/FAILED line, so it must not point "above" at lines that are not there
	// (g7 friction re-verification of F12) ...
	if strings.Contains(logs, "logged above") {
		t.Fatalf("the warning points at log lines above it, but it comes first:\n%s", logs)
	}
	// ... and the antivirus advice is the Windows sharing-violation remedy, not
	// advice for a permission or rename failure on another OS.
	if got := strings.Contains(logs, "antivirus"); got != (runtime.GOOS == "windows") {
		t.Fatalf("antivirus advice present=%v on %s:\n%s", got, runtime.GOOS, logs)
	}
}
