package wizard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
)

// writeDesktopOut writes a desktop-settings.json at path whose active archive is out.
func writeDesktopOut(t *testing.T, path, out string) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"version": 1, "out": out})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-667
// The wizard's health view reports on the archive the Desktop is set to (desktop-settings
// "out"), reconciling its own last-archive.txt breadcrumb to it, so an archive switched in the
// Desktop shows in the wizard instead of each surface tracking a different file (UX-2); with no
// Desktop pointer set (missing or empty "out"), it falls back to the breadcrumb unchanged.
func TestMA667_WizardReadsDesktopActiveArchive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())    // redirects os.UserConfigDir on darwin/linux
	t.Setenv("AppData", t.TempDir()) // and on windows
	dir, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	bc, err := lastArchiveFile()
	if err != nil {
		t.Fatal(err)
	}
	settings, err := job.DesktopSettingsFile()
	if err != nil {
		t.Fatal(err)
	}

	x := filepath.Join(t.TempDir(), "X") // what the wizard last touched
	y := filepath.Join(t.TempDir(), "Y") // what the Desktop is now set to

	// The breadcrumb points at X; the Desktop has since switched to Y.
	if err := os.WriteFile(bc, []byte(x+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeDesktopOut(t, settings, y)
	got := currentArchive()
	assure.Reached(t, got, "current archive")
	if got != y {
		t.Fatalf("the wizard must report the Desktop's active archive %q; got %q", y, got)
	}
	// The breadcrumb is reconciled to Y, so the two surfaces no longer drift.
	if data, _ := os.ReadFile(bc); strings.TrimSpace(string(data)) != y {
		t.Fatalf("last-archive.txt must be reconciled to %q; got %q", y, strings.TrimSpace(string(data)))
	}

	// With no Desktop settings file at all, the wizard uses its own breadcrumb.
	if err := os.Remove(settings); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bc, []byte(x+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := currentArchive(); got != x {
		t.Fatalf("with no Desktop pointer the wizard must use its breadcrumb %q; got %q", x, got)
	}

	// An empty "out" is also a fall-back to the breadcrumb (no spurious switch to "").
	writeDesktopOut(t, settings, "")
	if got := currentArchive(); got != x {
		t.Fatalf("an empty Desktop out must fall back to the breadcrumb %q; got %q", x, got)
	}
}
