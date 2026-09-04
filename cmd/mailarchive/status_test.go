package main

import (
	"encoding/json"
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

// covers: MA-196, R18, R20, S29, S34
// End-to-end: a PST-only archive (support.pst carries no preserved .eml) reports
// "Extractable: 0 of M" on status's extractability line with the source-aware
// wording — never a categorical "re-archive" verdict — and status -json carries
// an extractable object with records_with_eml=0 and records>0. The count is the
// same file-presence signal `verify`/`extract` use, computed through the shared
// app.ExtractableCount helper that status wires in.
func TestStatusExtractablePSTNone(t *testing.T) {
	out := t.TempDir()
	if code, stderr := runCLI("-input", "../../testdata/support.pst", "-out", out); code != 0 {
		t.Fatalf("export failed (%d): %s", code, stderr)
	}

	stdout, err := exec.Command(testBin, "status", "-out", out).Output()
	if err != nil {
		t.Fatalf("status refused a real archive: %v", err)
	}
	text := string(stdout)
	if !strings.Contains(text, "Extractable: 0 of ") {
		t.Errorf("a PST-only archive should report 0 extractable of M:\n%s", text)
	}
	if !strings.Contains(text, "keep the .pst") {
		t.Errorf("the wording should tell the operator to keep the .pst to migrate:\n%s", text)
	}
	if !strings.Contains(text, "-raw") {
		t.Errorf("the wording should name -raw for a raw-capable source:\n%s", text)
	}
	if strings.Contains(text, "re-archive") {
		t.Errorf("status must never print a categorical re-archive verdict:\n%s", text)
	}

	jb, err := exec.Command(testBin, "status", "-out", out, "-json").Output()
	if err != nil {
		t.Fatalf("status -json failed: %v", err)
	}
	var doc struct {
		Version     int `json:"version"`
		Extractable *struct {
			Records        int `json:"records"`
			RecordsWithEML int `json:"records_with_eml"`
		} `json:"extractable"`
	}
	if err := json.Unmarshal(jb, &doc); err != nil {
		t.Fatalf("status -json is not valid JSON: %v\n%s", err, jb)
	}
	if doc.Extractable == nil {
		t.Fatalf("status -json lacks the extractable object:\n%s", jb)
	}
	if doc.Extractable.RecordsWithEML != 0 {
		t.Errorf("PST-only records_with_eml = %d, want 0", doc.Extractable.RecordsWithEML)
	}
	if doc.Extractable.Records <= 0 {
		t.Errorf("records = %d, want the manifest's record count (>0)", doc.Extractable.Records)
	}
}
