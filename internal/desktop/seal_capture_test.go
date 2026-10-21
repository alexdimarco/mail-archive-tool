package desktop

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/seal"
)

// covers: MA-670
// The scheduled Microsoft 365 capture (HeadlessCapture) refuses a paused archive at
// its entry — before recordStartFailure or any write — with a typed, Resume-routing
// seal.ErrSealed and NO last-run record left in the archive (S2-C2).
func TestMA670_HeadlessCaptureRefusesSealedArchive(t *testing.T) {
	out := t.TempDir()
	if err := seal.Seal(out, "parked"); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Out: out, SettingsPath: filepath.Join(t.TempDir(), "s.json")}
	err := HeadlessCapture(cfg, io.Discard)
	if !errors.Is(err, seal.ErrSealed) {
		t.Fatalf("HeadlessCapture on a paused archive must refuse with ErrSealed, got %v", err)
	}
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names("paused", "resume"))
	if _, statErr := os.Stat(filepath.Join(out, ".mailarchive-lastrun.json")); statErr == nil {
		t.Fatal("a paused capture must write no last-run record (refused before recordStartFailure)")
	}
}
