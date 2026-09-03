package main

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-72, MA-99, R14, R12, S28
// A control character in any scheduled argument is refused at schedule time
// (a newline would inject a crontab line); and a job marked -unattended — as
// every scheduled job is — refuses to create a new archive when its -out does
// not exist (the backup drive is not mounted), instead of archiving onto the
// local disk and reporting success.
func TestScheduleControlCharsAndUnattendedOut(t *testing.T) {
	out := t.TempDir()
	code, stderr := runCLI("schedule", "-out", out+"\n* * * * * /tmp/pwn", "-auto")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("control character"))

	code, stderr = runCLI("schedule", "--", "-out", out, "-input", "a\rb.pst")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("control character"))

	missing := filepath.Join(t.TempDir(), "unmounted", "archive")
	code, stderr = runCLI("-unattended", "-input", "../../testdata/support.pst", "-out", missing)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("not present", "mounted"),
		assure.NoSideEffect(func() bool { _, err := os.Stat(missing); return os.IsNotExist(err) }))

	// The positive twin: an existing -out is accepted under -unattended.
	if code, stderr := runCLI("-unattended", "-input", "../../testdata/support.pst", "-out", out); code != 0 {
		t.Fatalf("unattended export into an existing directory refused (%d): %s", code, stderr)
	}
}
