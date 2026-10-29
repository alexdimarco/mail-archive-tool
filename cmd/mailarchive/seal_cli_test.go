package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/seal"
)

// covers: MA-682
// The interactive CLI export refuses a paused (sealed) archive at ENTRY, before the
// -outlook COM path recreates <out>/_outlook-pst inside it — the engine's own seal check in
// app.Run runs only after that pre-lock write. End-to-end through the built binary: a sealed
// archive makes `mailarchive -outlook -out DIR` refuse (naming the pause) and leaves no
// _outlook-pst behind. Encodes the 2026-10-20 security-pass finding (S2-C1/C2).
func TestMA682_ExportRefusesSealedArchiveBeforeWriting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("on Windows, -outlook drives real Outlook should the refusal ever not come first")
	}
	out := t.TempDir()
	if err := seal.Seal(out, "parked by the owner"); err != nil {
		t.Fatalf("seal: %v", err)
	}

	cmd := exec.Command(testBin, "-outlook", "-out", out)
	combined, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("export into a sealed archive must refuse, but it exited 0:\n%s", combined)
	}
	if !strings.Contains(string(combined), "paused") {
		t.Fatalf("the refusal must name the pause (not merely the platform), got:\n%s", combined)
	}
	if _, statErr := os.Stat(filepath.Join(out, "_outlook-pst")); !os.IsNotExist(statErr) {
		t.Fatal("a refused -outlook export must NOT create _outlook-pst inside the sealed archive")
	}
}
