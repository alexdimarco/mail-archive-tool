package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-396, R18, R12, S29, S9
// A scheduled export whose -input has disappeared is still a recorded run
// (N13): on an archive that already exists, `-unattended` refuses with exit 1
// naming the missing path AND finalizes the last-run record failed with that
// reason, drops the attention sidecar, logs FAILED to its -log, and `status`
// reports RED run_failed — instead of the previous success staying "ok". It
// archives nothing (the manifest is unchanged). Positive twin: the same
// scheduled job with its input present records ok.
func TestScheduledExportMissingInputIsRecorded(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	out := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "run.log")
	job := []string{"-out", out, "-input", src, "-unattended", "-log", logPath}

	if code, stderr := runCLI(job...); code != 0 {
		t.Fatalf("healthy scheduled run: exit %d\n%s", code, stderr)
	}
	first, st, err := state.ReadLastRun(out)
	if err != nil || st != state.LastRunPresent || first.Status != state.RunOK {
		t.Fatalf("healthy scheduled run recorded %+v (%v %v)", first, st, err)
	}
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	manifestBefore, err := os.ReadFile(mpath)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(src); err != nil {
		t.Fatal(err)
	}
	code, stderr := runCLI(job...)
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names(src, "does not exist"),
		assure.NoSideEffect(func() bool {
			now, err := os.ReadFile(mpath)
			return err == nil && bytes.Equal(now, manifestBefore) // nothing archived
		}))

	lr, st, err := state.ReadLastRun(out)
	if err != nil || st != state.LastRunPresent {
		t.Fatalf("no last-run record: %v %v", st, err)
	}
	if lr.Status != state.RunFailed || !strings.Contains(lr.Error, src) || !lr.Started.After(first.Started) {
		t.Fatalf("the refused scheduled run was not recorded: %+v (previous %+v)", lr, first)
	}
	if side, err := os.ReadFile(filepath.Join(out, state.AttentionName)); err != nil || !strings.Contains(string(side), src) {
		t.Errorf("attention sidecar missing or does not name the input: %v\n%s", err, side)
	}
	logged, _ := os.ReadFile(logPath)
	if !strings.Contains(string(logged), "FAILED:") || !strings.Contains(string(logged), src) {
		t.Errorf("the -log file has no FAILED line naming the input:\n%s", logged)
	}
	if posture, codes, reasons := statusDoc(t, out); posture != "RED" || !hasCode(codes, "run_failed") {
		t.Errorf("status after the refused scheduled run: %s %v %v", posture, codes, reasons)
	}
}
