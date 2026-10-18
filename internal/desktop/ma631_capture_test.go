package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-631, R14, R18, S59
// The capture leg of §4.6.5's kind guard: CaptureCorroborates holds only when
// Microsoft 365 is configured AND the desktop's archive (or its recorded
// BackupFor) is the archive asked about, reading owner-side config only; and the
// package init wires it into schedule's CaptureCorroborator seam so the guard
// sees captures in production.
func TestMA631_CaptureCorroborates(t *testing.T) {
	if schedule.CaptureCorroborator == nil {
		t.Fatal("init did not wire schedule.CaptureCorroborator")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	cfgPath, err := graphconfig.DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	archive := t.TempDir()

	// Not configured: no capture corroborated.
	if CaptureCorroborates(archive) {
		t.Error("with no Microsoft 365 config, no capture should be corroborated")
	}

	// Configure Microsoft 365 and point the desktop at the archive.
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(filepath.Join(filepath.Dir(cfgPath), "desktop-settings.json"), Settings{Out: archive}); err != nil {
		t.Fatal(err)
	}

	if !CaptureCorroborates(archive) {
		t.Error("a configured desktop pointed at the archive should corroborate a capture")
	}
	if CaptureCorroborates(t.TempDir()) {
		t.Error("a different archive is not corroborated")
	}
	if CaptureCorroborates("") {
		t.Error("an empty archive is not corroborated")
	}
}
