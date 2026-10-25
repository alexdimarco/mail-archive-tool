package backupalert

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// covers: MA-736, R37
// The alert email for an encrypted archive says which archive and the failure kind's
// fixed words — never the run error's text (a source path, a mailbox address, server
// text) nor how many messages the run archived (P2a-C16). The positive twin: the same
// failure of a plaintext archive sends its error text and its count as before.
func TestAnEncryptedArchivesAlertSaysOnlyTheKind(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(settingsPath, []byte(`{"alertEnabled":true,"alertOnFailure":true,"alertOnSuccess":true,"alertHost":"smtp.example.org","alertPort":587,"alertFrom":"b@example.org","alertTo":"t@example.org","alertUsername":"b@example.org"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var sent []alert.Event
	restore := SetSenderForTest(func(_ alert.Config, _ string, ev alert.Event) error { sent = append(sent, ev); return nil })
	defer restore()
	secret := errors.New("1 file(s) failed: /Users/someone/Mail/SECRET-STORE: permission denied")

	plain := t.TempDir()
	FireForRun(settingsPath, nil, plain, ResultFailed, secret, 0, nil)
	FireForRun(settingsPath, nil, plain, ResultOK, nil, 12, nil)
	if len(sent) != 2 || sent[0].Error != secret.Error() || sent[1].Messages != 12 {
		t.Fatalf("the positive twin: a plaintext archive's alert keeps its text and count: %+v", sent)
	}

	sealed := t.TempDir()
	p, err := vault.PrepareCreate(sealed, "alert test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	k, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
	sent = nil
	FireForRun(settingsPath, nil, sealed, ResultFailed, secret, 3, nil)
	FireForRun(settingsPath, nil, sealed, ResultOK, nil, 12, nil)
	if len(sent) != 2 {
		t.Fatalf("an encrypted archive's alerts must still be sent: %+v", sent)
	}
	if sent[0].Error != state.FailureText(app.FailureKind(secret)) || sent[0].Messages != 0 || sent[1].Messages != 0 || sent[0].Archive != sealed {
		t.Fatalf("an encrypted archive's alert carries more than its location and kind: %+v", sent)
	}
	for _, ev := range sent {
		if strings.Contains(ev.Error, "SECRET") || strings.Contains(ev.Error, "someone") {
			t.Fatalf("the alert carries the error's text: %+v", ev)
		}
	}
}
