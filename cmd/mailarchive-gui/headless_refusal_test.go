package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/state"
)

// covers: MA-397, R18, S29
// The GUI's headless (scheduled) run mirrors the CLI (N13): when the saved
// job's input has disappeared from an archive that already exists, app.Run's
// discovery refusal is recorded as a FAILED run — last-run failed naming the
// input, the attention sidecar written — rather than leaving the previous
// success as the archive's last word. Positive twin: the job with its input
// present records ok.
func TestHeadlessMissingInputIsRecorded(t *testing.T) {
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

	if code := runHeadless(jobPath); code != 0 {
		t.Fatalf("healthy headless run exit = %d, want 0", code)
	}
	first, st, err := state.ReadLastRun(out)
	if err != nil || st != state.LastRunPresent || first.Status != state.RunOK {
		t.Fatalf("healthy headless run recorded %+v (%v %v)", first, st, err)
	}

	if err := os.Remove(mbox); err != nil {
		t.Fatal(err)
	}
	if code := runHeadless(jobPath); code != 1 {
		t.Fatalf("headless run with a vanished input exit = %d, want 1", code)
	}
	lr, st, err := state.ReadLastRun(out)
	if err != nil || st != state.LastRunPresent {
		t.Fatalf("no last-run record: %v %v", st, err)
	}
	if lr.Status != state.RunFailed || !strings.Contains(lr.Error, mbox) || !lr.Started.After(first.Started) {
		t.Fatalf("the refused headless run was not recorded: %+v (previous %+v)", lr, first)
	}
	if side, err := os.ReadFile(filepath.Join(out, state.AttentionName)); err != nil || !strings.Contains(string(side), mbox) {
		t.Errorf("attention sidecar missing or does not name the input: %v\n%s", err, side)
	}

	// A directory that holds no archive yet gets nothing written: the refusal
	// of a first run still creates no last-run record or sidecar (MA-103).
	empty := filepath.Join(dir, "fresh")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	freshJob := filepath.Join(dir, "mailarchive-fresh.json")
	if err := job.Write(freshJob, job.Job{Name: "mailarchive-fresh", Out: empty, Mode: "incremental", Inputs: []string{mbox}}); err != nil {
		t.Fatal(err)
	}
	if code := runHeadless(freshJob); code != 1 {
		t.Fatalf("headless first run with a missing input exit = %d, want 1", code)
	}
	for _, name := range []string{state.LastRunName, state.AttentionName, ".mailarchive-manifest.json"} {
		if _, err := os.Stat(filepath.Join(empty, name)); !os.IsNotExist(err) {
			t.Errorf("a refused first run wrote %s into a directory with no archive", name)
		}
	}
}
