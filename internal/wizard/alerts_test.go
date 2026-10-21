package wizard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/job"
)

// covers: MA-702
// A scheduled LOCAL job (RunHeadless) emails its result through the shared backupalert
// layer, from the one configuration the dashboard saves — so a local backup alerts the
// same way the Microsoft 365 scheduled capture does. Here an absent -out (the backup drive
// not mounted) fires a failure alert.
func TestMA702_LocalJobAlerts(t *testing.T) {
	// Keep the failure toast out of the test (it would pop a real OS notification).
	oldNotify := notify
	notify = func(string, ...zenity.Option) error { return nil }
	defer func() { notify = oldNotify }()

	// Point the shared alert layer at a temp settings file with alerts on failure.
	settingsPath := filepath.Join(t.TempDir(), "desktop-settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"alertEnabled":true,"alertOnFailure":true,"alertHost":"smtp.office365.com","alertPort":587,"alertFrom":"b@sa.org","alertTo":"t@sa.org"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	oldPath := backupalert.DefaultSettingsPath
	backupalert.DefaultSettingsPath = func() string { return settingsPath }
	defer func() { backupalert.DefaultSettingsPath = oldPath }()

	var sent []alert.Event
	restore := backupalert.SetSenderForTest(func(_ alert.Config, _ string, ev alert.Event) error { sent = append(sent, ev); return nil })
	defer restore()

	jobPath := filepath.Join(t.TempDir(), "job.json")
	if err := job.Write(jobPath, job.Job{Name: "StA", Out: filepath.Join(t.TempDir(), "no-such-drive"), Mode: "incremental"}); err != nil {
		t.Fatal(err)
	}
	if code := RunHeadless(jobPath); code != 1 {
		t.Fatalf("an absent -out must fail the local job, got exit %d", code)
	}
	if len(sent) != 1 || sent[0].Result != backupalert.ResultFailed {
		t.Fatalf("the local job must email a failure alert, got %+v", sent)
	}
}
