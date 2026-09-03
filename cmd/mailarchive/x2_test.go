package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-159, R18, S29
// `status` on a directory whose manifest FILE exists but does not load (a newer
// format) reports a RED naming the format-version wording — it exits 0 (it
// reported) and does NOT refuse with the wrong "no archive … run an export
// first" advice (friction #7a).
func TestStatusUnreadableManifestNotNoArchive(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), []byte(`{"version":99,"entries":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLIOut(t, "status", "-out", out)
	if code != 0 {
		t.Fatalf("status refused a present-but-unreadable manifest (exit %d): %s", code, stderr)
	}
	if !strings.Contains(stdout, "RED") || !strings.Contains(stdout, "format version") {
		t.Errorf("status did not RED-name the newer-format manifest:\n%s", stdout)
	}
	if strings.Contains(stdout, "no archive") || strings.Contains(stderr, "no archive") {
		t.Errorf("status wrongly reported 'no archive' for a present manifest:\nout:%s\nerr:%s", stdout, stderr)
	}
}
