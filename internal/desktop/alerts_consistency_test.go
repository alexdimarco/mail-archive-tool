package desktop

import (
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/backupalert"
)

// covers: MA-702
// The Desktop writes the alert settings; the shared backupalert layer (which a scheduled
// local job reads) reads them from the same file. This guards that the JSON keys stay in
// step, so a local backup sees exactly what the dashboard saved.
func TestMA702_SettingsKeysConsistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	if err := SaveSettings(path, Settings{
		AlertEnabled: true, AlertOnFailure: true, AlertOnSuccess: true, AlertOnPause: true,
		AlertProvider: "gmail", AlertHost: "smtp.gmail.com", AlertPort: 587, AlertSecurity: "starttls",
		AlertFrom: "f@x", AlertTo: "t@x", AlertUsername: "u@x",
	}); err != nil {
		t.Fatal(err)
	}
	c := backupalert.LoadConfig(path)
	if !c.Enabled || !c.OnFailure || !c.OnSuccess || !c.OnPause ||
		c.Provider != "gmail" || c.Host != "smtp.gmail.com" || c.Port != 587 || c.Security != "starttls" ||
		c.From != "f@x" || c.To != "t@x" || c.Username != "u@x" {
		t.Fatalf("backupalert.LoadConfig did not read what the Desktop wrote (key drift?): %+v", c)
	}
}
