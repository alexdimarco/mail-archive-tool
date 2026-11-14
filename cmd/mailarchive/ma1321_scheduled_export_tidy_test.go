package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
)

// covers: MA-1321, R45, R37
// A backup a `mailarchive schedule` job runs (`export -unattended`) meets another computer's
// tidy-up as the scheduled run-job does (the B3 adversarial pass, round 2's G4): a skip, not a
// failure. It exits 0, says "Skipped, not failed:" and the tidy-up words (never "FAILED:"), writes
// no record into the archive, and keeps on this computer when the wait began, so `status` says the
// tidy-up WARN with "since DATE", never RED. (Such a job has no email alerts of its own.) Positive
// twin first: the same backup typed at a terminal still exits 1 with "Not backed up:" (MA-1313).
func TestAScheduledExportMeetingATidyUpIsASkip(t *testing.T) {
	for k, v := range cliEnv() {
		t.Setenv(k, v)
	}
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	dir := filepath.Join(t.TempDir(), "archive")
	if code, stderr := runCLI("-out", dir, "-input", src); code != 0 {
		t.Fatalf("the first backup: exit %d, %s", code, stderr)
	}
	stampManifest(t, dir, "8")
	before, _ := os.ReadFile(filepath.Join(dir, state.LastRunName))
	if code, stderr := runCLI("-out", dir, "-input", src); code != 1 || !strings.Contains(stderr, "Not backed up: "+state.TidyUpWords) {
		t.Fatalf("positive twin: a terminal backup exits %d, said:\n%s", code, stderr)
	}
	if _, ok := gmailanswer.ReadTidyWait(dir); ok {
		t.Fatal("positive twin: a terminal backup kept a scheduled backup's wait")
	}

	code, stderr := runCLI("-out", dir, "-input", src, "-unattended")
	if code != 0 || strings.Contains(stderr, "FAILED:") || !strings.Contains(stderr, "Skipped, not failed: "+state.TidyUpWords) {
		t.Fatalf("a scheduled export meeting the tidy-up: exit %d, said:\n%s", code, stderr)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, state.LastRunName)); string(after) != string(before) {
		t.Fatal("the skipped backup wrote a record into the archive")
	}
	w, ok := gmailanswer.ReadTidyWait(dir)
	if !ok {
		t.Fatal("the scheduled export kept no wait")
	}
	cmd := exec.Command(testBin, "status", "-out", dir)
	cmd.Env = os.Environ()
	got, _ := cmd.CombinedOutput()
	if s := string(got); strings.Contains(s, "RED") || !strings.Contains(s, "WARN: "+state.TidyUpWords) || !strings.Contains(s, state.TidyWaitSinceLine(w.Since)) {
		t.Fatalf("status after the skip:\n%s", s)
	}
}
