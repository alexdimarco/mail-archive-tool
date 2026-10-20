package app

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
)

// covers: MA-689
// PM4 on the scheduled path (archive-manager Slice 3c, S3-C4 — fixes a shipped-code bug): an
// UNATTENDED run (a scheduled backup) whose archive -out is not present — an unmounted backup
// drive, a down share — REFUSES before any MkdirAll, so it never recreates the archive on the
// local disk and silently backs up into a decoy, reporting success. An interactive run is not
// gated by this guard (it may create the archive on first use). Encodes the 2026-10-20 pre-code
// pass's 2nd critical.
func TestMA689_UnattendedRunRefusesMissingOut(t *testing.T) {
	// RequireExistingOut: a missing or non-dir out is refused; an existing dir passes.
	if RequireExistingOut(filepath.Join(t.TempDir(), "unmounted", "mail")) == nil {
		t.Fatal("a missing out must be refused")
	}
	if err := RequireExistingOut(t.TempDir()); err != nil {
		t.Fatalf("an existing out dir must pass: %v", err)
	}
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if RequireExistingOut(f) == nil {
		t.Fatal("a plain file (not a dir) must be refused")
	}

	// Wiring: an UNATTENDED run with a present input but a missing out refuses before creating
	// anything. The input exists so discovery passes; the out-guard is what refuses (PM4).
	in := filepath.Join(t.TempDir(), "mail.mbox")
	if err := os.WriteFile(in, []byte("From a@b\r\nSubject: x\r\n\r\nbody\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missingOut := filepath.Join(t.TempDir(), "gone-drive", "archive")
	_, err := Run(context.Background(), Options{Inputs: []string{in}, Out: missingOut, Mode: export.Incremental, Unattended: true}, log.New(io.Discard, "", 0), nil)
	if err == nil || !strings.Contains(err.Error(), "not present") {
		t.Fatalf("an unattended run with a missing out must refuse with 'not present', got %v", err)
	}
	if _, serr := os.Stat(missingOut); !os.IsNotExist(serr) {
		t.Fatal("a refused unattended run must NOT create the archive dir on the local disk (PM4)")
	}
}
