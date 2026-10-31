package desktop

import (
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-910, S65
// The failure option's checkbox names the sync email it also sends (design-presync-
// attended §5.5, C-29): "a failed backup, or Thunderbird not synced for 3 days
// (recommended)", on the box with id alertOnFailure, ticked as saved; the old label that
// named only a failed backup is gone.
func TestTheFailureCheckboxNamesTheSyncEmail(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "s.json")
	if err := SaveSettings(settingsPath, Settings{AlertEnabled: true, AlertOnFailure: true}); err != nil {
		t.Fatal(err)
	}
	page := (&Config{SettingsPath: settingsPath, Store: newMemStore(), ConfigPath: filepath.Join(t.TempDir(), "none.json")}).overview("csrf-token")
	const want = `<input type="checkbox" id="alertOnFailure" checked> a failed backup, or Thunderbird not synced for 3 days (recommended)</label>`
	if !strings.Contains(page, `id="sec-alerts"`) {
		t.Fatal("positive twin: the Backup alerts card is not on the page")
	}
	if !strings.Contains(page, want) {
		t.Errorf("the failure option's label is not the sync-naming one; want %q", want)
	}
	if strings.Contains(page, "> a failed backup (recommended)") {
		t.Error("the old label that names only a failed backup is still on the page")
	}
}
