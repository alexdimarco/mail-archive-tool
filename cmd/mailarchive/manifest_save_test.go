package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// statusDoc runs `status -json` on out and returns its posture and codes.
func statusDoc(t *testing.T, out string) (string, []string, []string) {
	t.Helper()
	jb, err := exec.Command(testBin, "status", "-out", out, "-json").Output()
	if err != nil {
		t.Fatalf("status -json failed: %v", err)
	}
	var doc struct {
		Posture     string   `json:"posture"`
		Reasons     []string `json:"reasons"`
		ReasonCodes []string `json:"reason_codes"`
	}
	if err := json.Unmarshal(jb, &doc); err != nil {
		t.Fatalf("status -json is not valid JSON: %v\n%s", err, jb)
	}
	return doc.Posture, doc.ReasonCodes, doc.Reasons
}

func hasCode(codes []string, code string) bool {
	for _, c := range codes {
		if c == code {
			return true
		}
	}
	return false
}

// covers: MA-393, R12, R18, S29
// End to end through the CLI, an export whose manifest cannot be saved exits
// with the typed error exit (1) and a `mailarchive:` message naming the
// manifest file and the remedy (F53) — never exit 0 — and `status` then
// reports the run as FAILED (RED run_failed naming the manifest) rather than
// the green "ok" that hid the lost progress. Positive twin: the same export
// with a writable manifest location exits 0 and status shows no run_failed.
func TestExportFailsWhenManifestCannotBeSaved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions model an unwritable manifest location")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)

	okOut := t.TempDir()
	if code, stderr := runCLI("-out", okOut, "-input", src, "-manifest", filepath.Join(t.TempDir(), "m.json")); code != 0 {
		t.Fatalf("healthy export: exit %d\n%s", code, stderr)
	}
	if _, codes, reasons := statusDoc(t, okOut); hasCode(codes, "run_failed") {
		t.Fatalf("healthy export reported run_failed: %v", reasons)
	}

	ro := t.TempDir()
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	mpath := filepath.Join(ro, "m.json")
	out := t.TempDir()
	code, stderr := runCLI("-out", out, "-input", src, "-manifest", mpath)
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names("mailarchive:", mpath, "could not save the manifest", "writable"),
		assure.NoSideEffect(func() bool {
			_, err := os.Stat(mpath)
			return os.IsNotExist(err)
		}))

	posture, codes, reasons := statusDoc(t, out)
	if posture != "RED" || !hasCode(codes, "run_failed") || !strings.Contains(strings.Join(reasons, "\n"), mpath) {
		t.Fatalf("status after a manifest-save failure: posture %s codes %v — want RED run_failed naming %s\n%v", posture, codes, mpath, reasons)
	}
}
