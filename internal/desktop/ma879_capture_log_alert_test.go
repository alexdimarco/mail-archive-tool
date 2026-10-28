package desktop

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/seal"
)

// covers: MA-879, MA-870, R37
// A scheduled capture refused because its log would be inside an encrypted archive goes through
// the alert streak like the capture's other failure exits (R3C-10; the part A pass): the streak
// beside the Desktop's settings is marked failing, so the failure is told once, never silently.
// The paused refusal stays out of it: no streak. Positive twin first: the capture's own early
// exit (Microsoft 365 not set up) marks the streak.
func TestACaptureRefusedForItsLogIsAlerted(t *testing.T) {
	env := &launchEnv{Stderr: io.Discard}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	settings := filepath.Join(dir, "desktop-settings.json") // the launch's own, beside the config
	plainArchive := func() string {
		p := t.TempDir()
		if err := os.WriteFile(filepath.Join(p, manifestName), []byte(`{"version":6,"entries":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	failing := func(out string) bool {
		b, err := os.ReadFile(backupalert.StreakPath(settings, backupalert.KeyFor(out)))
		return err == nil && strings.Contains(string(b), `"failing":true`)
	}

	twin := plainArchive()
	_ = launch(context.Background(), []string{"-capture", "-config", cfgPath, "-out", twin, "-log", filepath.Join(t.TempDir(), "capture.log")}, env)
	if !failing(twin) {
		t.Fatal("the positive twin: the not-set-up refusal did not mark the streak")
	}

	sealed := vaultedArchive(t)
	plain := plainArchive()
	err := launch(context.Background(), []string{"-capture", "-config", cfgPath, "-out", plain, "-log", filepath.Join(sealed, "other.log")}, env)
	if !errors.Is(err, app.ErrLogInEncryptedArchive) {
		t.Fatalf("a log inside an encrypted archive: want ErrLogInEncryptedArchive, got %v", err)
	}
	if !failing(plain) {
		t.Error("the refusal for a log inside an encrypted archive did not go through the alert streak")
	}

	paused := plainArchive()
	if err := seal.Seal(paused, "test"); err != nil {
		t.Fatal(err)
	}
	_ = launch(context.Background(), []string{"-capture", "-config", cfgPath, "-out", paused, "-log", filepath.Join(sealed, "paused.log")}, env)
	if _, err := os.Lstat(backupalert.StreakPath(settings, backupalert.KeyFor(paused))); !os.IsNotExist(err) {
		t.Errorf("the paused refusal went through the alert streak: %v", err)
	}
}
