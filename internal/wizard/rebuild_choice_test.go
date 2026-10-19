package wizard

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/job"
)

// covers: MA-386, R12, S33
// mailarchive-gui's owner can act on a damaged search index (g7 friction F3): the
// health view names a "Rebuild the search index" choice instead of a command
// line this program's owner does not have, the choice is offered exactly when the
// posture carries an index reason the rebuild fixes, it rebuilds the index, and a
// scheduled job that meets the damaged index logs the refusal in plain words
// naming that choice, with the technical text on the next line. Positive twin:
// a healthy archive's health view offers no rebuild.
func TestGUIOffersAndNamesTheRebuild(t *testing.T) {
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
	if code := RunHeadless(jobPath); code != 0 {
		t.Fatalf("healthy headless run exit = %d", code)
	}
	gather := func() (health.Input, health.Report) {
		in := health.Gather(out, "")
		in.Surface = "gui"
		return in, health.Assess(in, time.Now())
	}
	if _, rep := gather(); rebuildOffered(rep) {
		t.Fatalf("positive twin: a healthy archive is offered a rebuild: %v", rep.Reasons)
	}

	idx := filepath.Join(out, "search.db")
	if err := os.WriteFile(idx, bytes.Repeat([]byte("not a sqlite database "), 200), 0o644); err != nil {
		t.Fatal(err)
	}
	in, rep := gather()
	if !rebuildOffered(rep) {
		t.Fatalf("a damaged index is not offered a rebuild: %v", rep.Reasons)
	}
	text := strings.Join(health.Summary(in, rep), "\n")
	if !strings.Contains(text, `choose "`+rebuildChoice+`" below`) || strings.Contains(text, "mailarchive reindex") {
		t.Fatalf("the health view must name its own choice and no command:\n%s", text)
	}

	// The scheduled job meets the damaged index: plain words, detail below.
	if code := RunHeadless(jobPath); code != 1 {
		t.Fatalf("headless run over a damaged index exit = %d, want 1", code)
	}
	logb, err := os.ReadFile(filepath.Join(out, "mailarchive.log"))
	if err != nil {
		t.Fatal(err)
	}
	logs := string(logb)
	if !strings.Contains(logs, "FAILED: The search index in") || !strings.Contains(logs, rebuildChoice) || !strings.Contains(logs, "detail: ") {
		t.Fatalf("the job log does not put the refusal in plain words:\n%s", logs)
	}

	// The choice rebuilds the index and clears the reason.
	msgText, err := rebuildSearchIndex(out)
	if err != nil || !strings.Contains(msgText, "was rebuilt") {
		t.Fatalf("rebuild: %q %v", msgText, err)
	}
	if _, rep := gather(); rebuildOffered(rep) {
		t.Fatalf("the rebuild left an index reason: %v", rep.Reasons)
	}
}
