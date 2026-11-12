package backupalert

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// covers: MA-1159, R18, S29
// The job's log names the email that was sent as its subject says it (the 1.6.1 friction review's
// P9: mailarchive.log said "backup alert email sent (failed)" for a run that did not save
// everything): a partial run's "did not save everything", a busy skip's "skipped, the archive
// was still in use", never "(failed)" for either; an outright failure still "(failed)". Positive
// twin first. The skip is a real busy refusal (a real backup into an archive whose lock another
// holder has); the partial error is the run's own type.
func TestTheJobLogNamesTheAlertThatWasSent(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(settingsPath, []byte(`{"alertEnabled":true,"alertOnFailure":true,"alertHost":"smtp.example.org","alertPort":587,"alertFrom":"b@example.org","alertTo":"t@example.org"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(SetSenderForTest(func(alert.Config, string, alert.Event) error { return nil }))
	logged := func(runErr error) string {
		t.Helper()
		var b bytes.Buffer
		FireForRun(settingsPath, nil, t.TempDir(), ResultFailed, runErr, 0, log.New(&b, "", 0))
		return strings.TrimSpace(b.String())
	}
	if got := logged(errors.New("the archive drive is full")); got != "backup alert email sent (failed) to t@example.org" {
		t.Fatalf("the positive twin: %q", got)
	}
	partial := logged(&app.PartialError{Summary: app.GapSummary{FolderGaps: 1}})
	if partial != "backup alert email sent (did not save everything) to t@example.org" {
		t.Errorf("a partial run: %q", partial)
	}

	out := t.TempDir()
	mbox := filepath.Join(t.TempDir(), "Inbox")
	if err := os.WriteFile(mbox, []byte("From a@example.org Mon Mar  3 09:00:00 2025\nFrom: a@example.org\nSubject: one\nMessage-ID: <one@example.org>\n\nHello.\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	held, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	_, runErr := app.Run(context.Background(), app.Options{Inputs: []string{mbox}, Out: out}, log.New(io.Discard, "", 0), nil)
	held.Release()
	if _, ok := app.BusySkipped(runErr); !ok {
		t.Fatalf("want a busy refusal, got %v", runErr)
	}
	skipped := logged(runErr)
	assure.Reached(t, skipped == "backup alert email sent (skipped, the archive was still in use) to t@example.org" && !strings.Contains(partial+skipped, "(failed)"),
		"a skip's log line: "+skipped)
}
