package wizard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/job"
)

// covers: MA-397, R18, S29
// The headless job's log never says "Done." above a FAILED line (g7 friction
// F11): a refused or failed run printed "Done. exported=0 ..." as if it had
// finished, and the owner reading mailarchive.log met Done before FAILED. The
// summary is logged only when the run finished (or was cancelled). Positive twin:
// the healthy run logs its "Done." line.
func TestHeadlessLogsDoneOnlyForARunThatFinished(t *testing.T) {
	old := notify
	notify = func(string, ...zenity.Option) error { return nil }
	defer func() { notify = old }()
	fdir := t.TempDir()
	oldDir := failureDir
	failureDir = func() (string, error) { return fdir, nil }
	defer func() { failureDir = oldDir }()

	dir := t.TempDir()
	out := filepath.Join(dir, "archive")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	mbox := filepath.Join(dir, "Inbox")
	const msg = "From - Mon Jan 01 00:00:00 2024\r\nFrom: Bob <bob@example.com>\r\nTo: Me <me@example.com>\r\n" +
		"Subject: Hello\r\nDate: Mon, 01 Jan 2024 10:00:00 +0000\r\nMessage-ID: <abc@example.com>\r\n\r\nplain body\r\n"
	if err := os.WriteFile(mbox, []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	jobPath := filepath.Join(dir, "mailarchive-nightly.json")
	if err := job.Write(jobPath, job.Job{Name: "mailarchive-nightly", Out: out, Mode: "incremental", Inputs: []string{mbox}}); err != nil {
		t.Fatal(err)
	}
	logOf := func() string {
		b, err := os.ReadFile(filepath.Join(out, "mailarchive.log"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	if code := RunHeadless(jobPath); code != 0 {
		t.Fatalf("healthy headless run exit = %d, want 0", code)
	}
	if n := strings.Count(logOf(), "Done. exported="); n != 1 {
		t.Fatalf("positive twin: the healthy run logged %d Done lines, want 1:\n%s", n, logOf())
	}

	if err := os.Remove(mbox); err != nil {
		t.Fatal(err)
	}
	if code := RunHeadless(jobPath); code != 1 {
		t.Fatalf("headless run with a vanished input exit = %d, want 1", code)
	}
	got := logOf()
	if !strings.Contains(got, "FAILED:") {
		t.Fatalf("the refused run logged no FAILED line:\n%s", got)
	}
	if n := strings.Count(got, "Done. exported="); n != 1 {
		t.Fatalf("a refused run logged \"Done.\" above its FAILED line (%d Done lines in all):\n%s", n, got)
	}
}
