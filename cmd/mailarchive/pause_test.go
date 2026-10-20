package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/seal"
)

// covers: MA-676
// The `pause` and `resume` CLI verbs make an archive read-only and writable again,
// end-to-end through the built binary (the headless un-seal path, S2-C4).
func TestMA676_PauseResumeCLI(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), []byte(`{"version":6,"stores":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if o, err := exec.Command(testBin, "pause", "-out", out, "-reason", "parked").CombinedOutput(); err != nil {
		t.Fatalf("`pause` failed: %v\n%s", err, o)
	}
	if !seal.IsSealed(out) {
		t.Fatal("after `pause` the archive must be sealed (read-only)")
	}

	if o, err := exec.Command(testBin, "resume", "-out", out).CombinedOutput(); err != nil {
		t.Fatalf("`resume` failed: %v\n%s", err, o)
	}
	if seal.IsSealed(out) {
		t.Fatal("after `resume` the archive must be writable again")
	}

	// -out is required.
	if _, err := exec.Command(testBin, "pause").CombinedOutput(); err == nil {
		t.Fatal("`pause` with no -out must fail")
	}
}
