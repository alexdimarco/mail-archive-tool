package backupalert

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/alert"
)

// covers: MA-702
// The shared fire layer: a missing settings file is a disabled config; a saved one loads;
// FireForRun honours the event gate; SendTest always sends but validates first.
func TestMA702_SharedFireLayer(t *testing.T) {
	if LoadConfig(filepath.Join(t.TempDir(), "none.json")).Enabled {
		t.Fatal("a missing settings file must be a disabled config")
	}
	settingsPath := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(settingsPath, []byte(`{"alertEnabled":true,"alertOnFailure":true,"alertHost":"smtp.office365.com","alertPort":587,"alertFrom":"b@sa.org","alertTo":"t@sa.org","alertUsername":"b@sa.org"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := LoadConfig(settingsPath); !c.Enabled || !c.OnFailure || c.Host != "smtp.office365.com" || c.Port != 587 || c.To != "t@sa.org" {
		t.Fatalf("LoadConfig mapped wrong: %+v", c)
	}

	var sent []alert.Event
	restore := SetSenderForTest(func(_ alert.Config, _ string, ev alert.Event) error { sent = append(sent, ev); return nil })
	defer restore()

	// The gate: a failure sends (OnFailure on), a success does not (OnSuccess off).
	FireForRun(settingsPath, nil, "/arch", ResultFailed, errors.New("drive gone"), 0, nil)
	if len(sent) != 1 || sent[0].Result != ResultFailed || sent[0].Error == "" {
		t.Fatalf("a failure must send: %+v", sent)
	}
	sent = nil
	FireForRun(settingsPath, nil, "/arch", ResultOK, nil, 0, nil)
	if len(sent) != 0 {
		t.Fatal("a success must not send when OnSuccess is off")
	}

	// SendTest sends regardless of the toggle, and refuses an incomplete config.
	sent = nil
	if err := SendTest(settingsPath, nil, "/arch", nil); err != nil || len(sent) != 1 {
		t.Fatalf("SendTest should send once: err=%v sent=%d", err, len(sent))
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"alertHost":"h"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SendTest(bad, nil, "/arch", nil); err == nil {
		t.Fatal("SendTest must refuse a config with no From/To/port")
	}
}
