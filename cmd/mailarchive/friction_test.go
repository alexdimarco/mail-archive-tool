package main

import (
	"bytes"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
)

// covers: MA-104, R1, R12
// printSummary prints ONE coherent Verification line — still-missing / source-
// empty / not-yet-re-examined / re-examined=<Retried> / a report path only when
// a report exists (friction #11) — and one -raw no-op WARNING when raw was asked
// for and messages were exported but no .eml was written (P7). The "Done."
// counts stay on their own single line (X6).
func TestPrintSummaryVerificationAndRawWarning(t *testing.T) {
	render := func(r app.Result, keepRaw bool) string {
		var buf bytes.Buffer
		printSummary(log.New(&buf, "", 0), r, true, keepRaw, "/tmp/archive")
		return buf.String()
	}

	// Gaps + a re-examination + a report present.
	out := render(app.Result{
		Stats:        export.Stats{Exported: 4, Retried: 3, RawWritten: 4},
		Fillable:     2,
		Terminal:     1,
		Unknown:      1,
		ManifestSize: 10,
		ReportPath:   "/tmp/archive/attachments-report.tsv",
	}, true)
	for _, want := range []string{
		"2 message(s) still missing content",
		"1 source-empty (never fillable)",
		"1 not yet re-examined",
		"re-examined=3",
		"details in /tmp/archive/attachments-report.tsv",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("verification line lacks %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "Verification:"); n != 1 {
		t.Errorf("expected exactly one Verification line, got %d:\n%s", n, out)
	}
	if strings.Contains(out, "-raw had no effect") {
		t.Errorf("-raw WARNING fired though .eml were written:\n%s", out)
	}

	// A clean re-examination with nothing left to report: the line still shows
	// the re-examination and cites no report path (no report file exists).
	clean := render(app.Result{Stats: export.Stats{Exported: 5, Retried: 5, RawWritten: 5}, ManifestSize: 5}, true)
	if !strings.Contains(clean, "re-examined=5") || strings.Contains(clean, "details in") {
		t.Errorf("clean re-examination line wrong:\n%s", clean)
	}

	// -raw asked for, messages exported, zero .eml (an Outlook source): WARNING.
	warn := render(app.Result{Stats: export.Stats{Exported: 6, RawWritten: 0}, ManifestSize: 6}, true)
	if !strings.Contains(warn, "-raw had no effect") {
		t.Errorf("expected the -raw no-op WARNING:\n%s", warn)
	}
	// Without -raw, no WARNING even when RawWritten==0.
	noflag := render(app.Result{Stats: export.Stats{Exported: 6, RawWritten: 0}, ManifestSize: 6}, false)
	if strings.Contains(noflag, "-raw had no effect") {
		t.Errorf("-raw WARNING fired without -raw:\n%s", noflag)
	}
}

// covers: MA-105, R12
// export -list previews the stores that would be archived — one per line with a
// rough size — and exits 0 WITHOUT exporting or creating the output directory
// (friction #5).
func TestExportListPreviewsWithoutCreating(t *testing.T) {
	src := t.TempDir()
	cur := filepath.Join(src, "cur")
	if err := os.MkdirAll(cur, 0o755); err != nil {
		t.Fatal(err)
	}
	msg := "From: a@example.com\r\nSubject: hi\r\nMessage-ID: <a@x>\r\n\r\nbody\r\n"
	if err := os.WriteFile(filepath.Join(cur, "1.eml"), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "archive") // must NOT be created

	cmd := exec.Command(testBin, "-input", src, "-out", out, "-list")
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("-list exited non-zero: %v", err)
	}
	if !strings.Contains(string(stdout), src) {
		t.Errorf("-list did not name the store %q:\n%s", src, stdout)
	}
	if _, serr := os.Stat(out); !os.IsNotExist(serr) {
		t.Errorf("-list created the output directory %s", out)
	}
}

// covers: MA-105, R12
// -list resolves the human-readable store label beside an opaque path: an
// Evolution IMAP cache directory is named by an account-UID hash on disk, so the
// preview shows the account's DisplayName (read from the small .source keyfile,
// not by opening the mail store) rather than the bare hash (friction #21). A
// path with no derivable friendly label yields none.
func TestListResolvesEvolutionLabel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows

	const uid = "3c07b233abcdef0123456789"
	cacheDir := filepath.Join(home, ".cache", "evolution", "mail", uid)
	cur := filepath.Join(cacheDir, "folders", "INBOX", "cur")
	if err := os.MkdirAll(cur, 0o755); err != nil {
		t.Fatal(err)
	}
	msg := "From: a@example.com\r\nSubject: hi\r\nMessage-ID: <a@x>\r\n\r\nbody\r\n"
	if err := os.WriteFile(filepath.Join(cur, "1"), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(home, ".config", "evolution", "sources")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, uid+".source"), []byte("[Data Source]\nDisplayName=Work IMAP\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := storeLabel(cacheDir); got != "Work IMAP" {
		t.Errorf("Evolution cache label = %q, want %q (the account name, not the hash %q)", got, "Work IMAP", filepath.Base(cacheDir))
	}
	if got := storeLabel(filepath.Join(home, "no-such-dir")); got != "" {
		t.Errorf("storeLabel for a missing path = %q, want empty", got)
	}
}

// covers: MA-110, R5
// reclaimPSTDir removes the temporary -outlook PST scratch directory only after
// a clean run, reporting the bytes reclaimed; a failed (or cancelled) run leaves
// it in place for retry or inspection (P2).
func TestReclaimPSTDir(t *testing.T) {
	// A dir with files + a nil run error: removed, byte count reported.
	dir := filepath.Join(t.TempDir(), "_outlook-pst")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "acct.pst"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	n, ok := reclaimPSTDir(dir, nil)
	if !ok || n < 4096 {
		t.Errorf("clean run: reclaimed=%d ok=%v, want >=4096 and true", n, ok)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("clean run did not remove %s", dir)
	}

	// A failed run: the dir is kept untouched.
	dir2 := filepath.Join(t.TempDir(), "_outlook-pst")
	if err := os.MkdirAll(dir2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "acct.pst"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, ok = reclaimPSTDir(dir2, errors.New("boom"))
	if ok || n != 0 {
		t.Errorf("failed run: reclaimed=%d ok=%v, want 0 and false", n, ok)
	}
	if _, err := os.Stat(dir2); err != nil {
		t.Errorf("failed run removed %s (should keep it): %v", dir2, err)
	}
}
