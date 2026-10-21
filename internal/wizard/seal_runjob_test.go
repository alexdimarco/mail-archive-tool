package wizard

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/seal"
)

// covers: MA-670
// The scheduled run-job (RunHeadless) refuses a paused archive after confirming the
// folder is present and BEFORE opening the run log or recreating PSTs — the writes
// that precede the engine lock — exiting non-zero with a Resume-routing stderr line
// and leaving NO mailarchive.log in the archive (S2-C2).
func TestMA670_RunJobRefusesSealedArchive(t *testing.T) {
	out := t.TempDir()
	if err := seal.Seal(out, "parked"); err != nil {
		t.Fatal(err)
	}
	jobPath := filepath.Join(t.TempDir(), "backup.json")
	if err := job.Write(jobPath, job.Job{Name: "backup", Out: out, Mode: "incremental", Inputs: []string{t.TempDir()}}); err != nil {
		t.Fatal(err)
	}

	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w
	rc := RunHeadless(jobPath)
	_ = w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if rc != 1 {
		t.Fatalf("RunHeadless on a paused archive must refuse (exit 1), got %d", rc)
	}
	if _, statErr := os.Stat(filepath.Join(out, "mailarchive.log")); statErr == nil {
		t.Fatal("a paused run-job must not open the run log in the archive (refused before runlog.Open)")
	}
	if msg := buf.String(); !strings.Contains(msg, "paused") || !strings.Contains(msg, "resume") {
		t.Fatalf("the refusal must name the pause and route to Resume, got: %q", msg)
	}
}
