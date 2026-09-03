package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-75, R18, R12, S29
// `status` on a real archive reports and exits 0; on a directory with neither
// manifest nor descriptor it refuses naming the directory.
func TestStatusCLI(t *testing.T) {
	out := t.TempDir()
	if code, stderr := runCLI("-input", "../../testdata/support.pst", "-out", out); code != 0 {
		t.Fatalf("export failed (%d): %s", code, stderr)
	}
	stdout, err := exec.Command(testBin, "status", "-out", out).Output()
	if err != nil {
		t.Fatalf("status refused a real archive: %v", err)
	}
	for _, want := range []string{"Archive:", "Messages:", "Last run:", "Schedule:", "Posture:"} {
		if !strings.Contains(string(stdout), want) {
			t.Errorf("status output lacks %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(string(stdout), "ok") {
		t.Errorf("status did not report the successful run:\n%s", stdout)
	}

	empty := t.TempDir()
	code, stderr := runCLI("status", "-out", empty)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names(filepath.Base(empty), "no manifest"))

	code, stderr = runCLI("status")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-out"))
}
