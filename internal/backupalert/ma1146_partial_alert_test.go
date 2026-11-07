package backupalert

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
)

// covers: MA-1146, R18, S48
// A scheduled run that ended partial is alerted as it was (the failing streak's email), and the
// event says it was partial, so the email's subject says the backup did not save everything.
// Positive twin first: an outright failure's event is not marked partial.
func TestAPartialRunsAlertIsMarkedPartial(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(settingsPath, []byte(`{"alertEnabled":true,"alertOnFailure":true,"alertHost":"smtp.example.org","alertPort":587,"alertFrom":"b@example.org","alertTo":"t@example.org"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var sent []alert.Event
	t.Cleanup(SetSenderForTest(func(_ alert.Config, _ string, ev alert.Event) error { sent = append(sent, ev); return nil }))
	out := t.TempDir()
	FireForRun(settingsPath, nil, out, ResultFailed, errors.New("the archive drive is full"), 0, nil)
	if len(sent) != 1 || sent[0].Partial {
		t.Fatalf("the positive twin: %+v", sent)
	}
	partial := &app.PartialError{Summary: app.GapSummary{ItemGaps: 1}}
	FireForRun(settingsPath, nil, out, ResultFailed, partial, 0, nil)
	assure.Reached(t, len(sent) == 2 && sent[1].Partial && sent[1].Error == partial.Error(), "the partial run's alert is sent and marked partial")
}
