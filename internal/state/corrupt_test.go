package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-94, R5, R12, S2
// A corrupt or truncated manifest (a torn write on a filesystem without fsync, a
// hand edit) is refused with an error that names the file and the remedy —
// restore it from a backup, or delete it to re-export — never a crash and never
// a silent "start from empty" that would re-export everything unannounced.
func TestCorruptManifestRefusedLegibly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"entries":{"a b":{"path":"x.html","fol`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err == nil {
		t.Fatalf("truncated manifest loaded without error: %+v", m)
	}
	msg := err.Error()
	for _, want := range []string{path, "delete", "restore"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal must name %q; got: %s", want, msg)
		}
	}
	if strings.Contains(msg, "panic") || strings.Contains(msg, "goroutine") {
		t.Errorf("refusal leaked a crash: %s", msg)
	}
}
