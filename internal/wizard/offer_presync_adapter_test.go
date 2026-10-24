//go:build darwin || windows

package wizard

import (
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-574
// On an adapter platform, the wizard offers the pre-sync only when a Thunderbird
// store is among the inputs (a directory under a profile that holds prefs.js),
// never for unrelated inputs.
func TestPresyncOfferable(t *testing.T) {
	base := t.TempDir()
	profile := filepath.Join(base, "abc.default")
	store := filepath.Join(profile, "Mail", "Local Folders")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte("// prefs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !presyncOfferable([]string{store}) {
		t.Error("a Thunderbird store among the inputs should make the pre-sync offerable")
	}
	// A separate temp root with no prefs.js anywhere above it.
	if presyncOfferable([]string{t.TempDir()}) {
		t.Error("a non-Thunderbird input should not make the pre-sync offerable")
	}
}
