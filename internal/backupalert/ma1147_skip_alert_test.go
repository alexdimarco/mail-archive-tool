package backupalert

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// covers: MA-1147, R18, S29
// A scheduled run refused because another run holds the archive (one that started a day or more
// ago: the only holder whose refusal reaches the alert) is alerted as it was, and the event says
// it was a skip, so the email does not say FAILED (friction review of plain-failure-words).
// Positive twin first: an outright failure's event is not marked as a skip. The busy refusal is
// a real one: a real backup into an archive whose lock another holder has.
func TestASkippedRunsAlertIsMarkedBusy(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(settingsPath, []byte(`{"alertEnabled":true,"alertOnFailure":true,"alertHost":"smtp.example.org","alertPort":587,"alertFrom":"b@example.org","alertTo":"t@example.org"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var sent []alert.Event
	t.Cleanup(SetSenderForTest(func(_ alert.Config, _ string, ev alert.Event) error { sent = append(sent, ev); return nil }))
	out := t.TempDir()
	FireForRun(settingsPath, nil, out, ResultFailed, errors.New("the archive drive is full"), 0, nil)
	if len(sent) != 1 || sent[0].Busy {
		t.Fatalf("the positive twin: %+v", sent)
	}

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
	FireForRun(settingsPath, nil, out, ResultFailed, runErr, 0, nil)
	assure.Reached(t, len(sent) == 2 && sent[1].Busy && !sent[1].Partial && sent[1].Error == runErr.Error(),
		"the skipped run's alert is sent and marked as a skip")
}
