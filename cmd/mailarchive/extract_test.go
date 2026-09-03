package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// writeMaildirMsg writes one message into a maildir folder (a raw-capable source).
func writeMaildirMsg(t *testing.T, folder, name, raw string) {
	t.Helper()
	cur := filepath.Join(folder, "cur")
	if err := os.MkdirAll(cur, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cur, name), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

const sampleMsg = "From: alice@example.com\r\nTo: bob@example.com\r\nSubject: Hi\r\n" +
	"Date: Mon, 03 Mar 2025 09:00:00 +0000\r\nMessage-ID: <hi@ex>\r\n\r\nBody text.\r\n"

// covers: MA-185, R20, R12, S34
// `schedule -- extract` is refused with a typed non-zero exit calling extract an
// operator-driven migration, not a backup; and `extract -h` names -out, -format
// and -dest and the lock/backup-window posture (X3).
func TestExtractNotSchedulableAndHelp(t *testing.T) {
	code, stderr := runCLI("schedule", "--", "extract", "-out", t.TempDir())
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("extract", "migration", "backup"))

	_, help := runCLI("extract", "-h")
	for _, w := range []string{"-out", "-format", "-dest", "mbox", "eml", "backup window"} {
		if !strings.Contains(help, w) {
			t.Errorf("extract -h does not name %q; got:\n%s", w, help)
		}
	}
}

// covers: MA-183, R20, R12, S34
// extract's exit status is the process contract (X1): a PST-only archive (no
// preserved originals) emits nothing and exits the partial code 3 — never 0,
// never verify's 2 — naming the skipped records; a raw archive with everything
// emitted exits 0. A -dest overlapping -out is a refusal (exit 1).
func TestExtractExitCodes(t *testing.T) {
	// PST archive → nothing extractable → exit 3.
	pstOut := t.TempDir()
	if code, _ := runCLI("-input", "../../testdata/support.pst", "-out", pstOut, "-raw"); code != 0 {
		t.Fatalf("pst export failed (%d)", code)
	}
	code, stdout, _ := runCLIOut(t, "extract", "-out", pstOut, "-format", "mbox", "-dest", t.TempDir())
	if code != 3 {
		t.Errorf("PST-only extract exit = %d, want 3 (partial); stdout:\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "skipped=") || !strings.Contains(stdout, "NOTHING extractable") {
		t.Errorf("partial run did not name the skipped records:\n%s", stdout)
	}

	// Raw maildir archive → everything emitted → exit 0.
	src := filepath.Join(t.TempDir(), "Alpha")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	rawOut := t.TempDir()
	if code, _ := runCLI("-input", src, "-out", rawOut, "-raw"); code != 0 {
		t.Fatalf("raw export failed (%d)", code)
	}
	code, stdout, _ = runCLIOut(t, "extract", "-out", rawOut, "-format", "eml", "-dest", t.TempDir())
	if code != 0 {
		t.Errorf("a fully-extractable archive exit = %d, want 0; stdout:\n%s", code, stdout)
	}

	// -dest overlapping -out is a refusal (exit 1), not a partial.
	code, _, stderr := runCLIOut(t, "extract", "-out", rawOut, "-format", "eml", "-dest", filepath.Join(rawOut, "inside"))
	if code != 1 {
		t.Errorf("overlapping -dest exit = %d, want 1 (refusal)", code)
	}
	if !strings.Contains(stderr, "-dest") {
		t.Errorf("overlap refusal does not name -dest: %s", stderr)
	}
}

// covers: MA-186, R20, S34
// The capture-time warning fires when a raw-capable source is archived WITHOUT
// -raw (extract would find nothing), and is ABSENT with -raw — so the operator
// can choose before deleting the source (PC15).
func TestExtractCaptureRawWarning(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Alpha")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)

	// Without -raw: the warning fires.
	_, stderr := runCLI("-input", src, "-out", t.TempDir())
	if !strings.Contains(stderr, "extract` will produce nothing") || !strings.Contains(stderr, "-raw") {
		t.Errorf("capture-time -raw warning did not fire for a raw-capable source without -raw:\n%s", stderr)
	}

	// With -raw: the warning is absent (the originals are preserved).
	_, stderr = runCLI("-input", src, "-out", t.TempDir(), "-raw")
	if strings.Contains(stderr, "extract` will produce nothing") {
		t.Errorf("capture-time -raw warning wrongly fired with -raw set:\n%s", stderr)
	}
}
