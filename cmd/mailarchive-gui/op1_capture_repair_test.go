package main

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-645, R14, R18
// The wizard's Repair rebuilds a -job schedule, so it must never offer to
// repair a schedule MailArchive Desktop installed (a -capture / desktop
// -capture descriptor): doing so would convert the desktop's weekly backup into
// a job-file schedule that fails every run (OP-1, R14, R18). A wizard job-file
// descriptor stays repairable under the existing host/exe conditions, and the
// health view names MailArchive Desktop as where to repair a capture schedule.
func TestWizardDoesNotRepairACaptureSchedule(t *testing.T) {
	t.Skip("xfail: OP-1 (R14, R18) — the capture-kind guard is not wired until the fix commit removes this skip")

	base := health.Input{HasDescriptor: true, ExeExists: true, ExeIsThis: false} // the program moved: a wizard schedule would be repairable

	capture := base
	capture.Desc = schedule.Descriptor{Job: []string{"-capture", "-config", "c", "-out", "o"}}
	if repairable(capture) {
		t.Error("a -capture descriptor is repairable by the wizard — it would convert the desktop's backup")
	}

	captureVerb := base
	captureVerb.Desc = schedule.Descriptor{Job: []string{"desktop", "-capture", "-out", "o"}}
	if repairable(captureVerb) {
		t.Error("a `desktop -capture` descriptor is repairable by the wizard")
	}

	if h := captureRepairHint(capture); !strings.Contains(h, "MailArchive Desktop") {
		t.Errorf("capture health hint %q does not name MailArchive Desktop", h)
	}

	// Positive twin: the wizard's own job-file schedule stays repairable, no hint.
	jobDesc := base
	jobDesc.Desc = schedule.Descriptor{Job: []string{"-job", "/path/job.json"}}
	if !repairable(jobDesc) {
		t.Error("a -job descriptor is not repairable by the wizard (regression)")
	}
	if h := captureRepairHint(jobDesc); h != "" {
		t.Errorf("a -job descriptor produced a capture hint %q", h)
	}
}
