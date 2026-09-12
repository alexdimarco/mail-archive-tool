package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-216, R14, S28
// The schedule preview surfaces the Deleted-Items/Junk choice for a graph job:
// with neither flag it names the default exclusion AND the opt-in flags (so the
// operator sees the default they are getting, not silence), and opting both in
// carries the flags into the previewed command line. A non-graph (export) job
// carries no such note.
func TestSchedulePreviewSurfacesDeletedJunkChoice(t *testing.T) {
	out := t.TempDir()
	secret := filepath.Join(t.TempDir(), "graph.secret")
	if err := os.WriteFile(secret, []byte("s3cr3t"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := []string{"schedule", "--", "graph",
		"-out", out, "-tenant", "contoso.com", "-client-id", "APPID",
		"-mailbox", "a@contoso.com", "-client-secret-file", secret}

	// Default: the preview names the default exclusion and the opt-in flags, and
	// the previewed command carries NEITHER flag.
	code, stdout, stderr := runCLIOut(t, base...)
	if code != 0 {
		t.Fatalf("schedule preview exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "excluded (the default)") {
		t.Errorf("default preview does not state the exclusion default:\n%s", stdout)
	}
	if !strings.Contains(stdout, "-include-deleted") || !strings.Contains(stdout, "-include-junk") {
		t.Errorf("default preview does not name the opt-in flags:\n%s", stdout)
	}

	// Opt in to both: the flags ride into the previewed command line, and the note
	// reports them included.
	optIn := append(append([]string{}, base...), "-include-deleted", "-include-junk")
	code, stdout, stderr = runCLIOut(t, optIn...)
	if code != 0 {
		t.Fatalf("schedule preview (opt-in) exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "-include-deleted") || !strings.Contains(stdout, "-include-junk") {
		t.Errorf("opted-in preview does not carry the flags into the command:\n%s", stdout)
	}
	if !strings.Contains(stdout, "included") {
		t.Errorf("opted-in preview note does not report them included:\n%s", stdout)
	}

	// A non-graph (export) job carries no trash/junk note.
	code, stdout, stderr = runCLIOut(t, "schedule", "-out", out, "-auto")
	if code != 0 {
		t.Fatalf("export schedule preview exited %d: %s", code, stderr)
	}
	if strings.Contains(stdout, "Deleted Items and Junk Email") {
		t.Errorf("an export job preview must not carry the trash/junk note:\n%s", stdout)
	}
}
