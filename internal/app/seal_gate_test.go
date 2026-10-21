package app

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/seal"
)

func sealRefusal(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	return 1, err.Error()
}

// covers: MA-669
// A paused archive refuses every content-mutating engine operation (export, graph
// capture, reindex redaction, verify -record) at its entry with a typed, Resume-routing
// refusal and NO last-run record, while a read is ALLOWED (a plain verify, and an index
// Rebuild that only regenerates derived data). The gate is verb/intent-aware — Reindex
// refuses, Rebuild allows, though both take the "reindex" lock — not a blanket
// AcquireAs check (archive-manager S2-C1, R35).
func TestMA669_SealedArchiveRefusesWrites(t *testing.T) {
	dir := t.TempDir()
	if err := seal.Seal(dir, "parked for the test"); err != nil {
		t.Fatal(err)
	}
	disc := log.New(io.Discard, "", 0)
	ctx := context.Background()

	// export / incremental — refused before any write, lock, or last-run record.
	_, err := Run(ctx, Options{Out: dir, Inputs: []string{t.TempDir()}}, disc, nil)
	if !errors.Is(err, seal.ErrSealed) {
		t.Fatalf("Run on a paused archive must refuse with ErrSealed, got %v", err)
	}
	rc, msg := sealRefusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("paused", "resume"))
	if _, statErr := os.Stat(filepath.Join(dir, ".mailarchive-lastrun.json")); statErr == nil {
		t.Fatal("a refused run must write no last-run record (refused before any write)")
	}

	// graph capture — refused before the interactive sign-in.
	if _, err := RunGraph(ctx, GraphOptions{}, Options{Out: dir}, disc); !errors.Is(err, seal.ErrSealed) {
		t.Fatalf("RunGraph on a paused archive must refuse with ErrSealed, got %v", err)
	}

	// reindex redaction — refused.
	if _, _, err := Reindex(dir, disc); !errors.Is(err, seal.ErrSealed) {
		t.Fatalf("Reindex (redaction) on a paused archive must refuse with ErrSealed, got %v", err)
	}

	// verify -record (writes fixity into the manifest) — refused.
	if _, err := Verify(dir, VerifyOptions{Record: true}, disc, nil); !errors.Is(err, seal.ErrSealed) {
		t.Fatalf("Verify -record on a paused archive must refuse with ErrSealed, got %v", err)
	}

	// ALLOWED on a paused archive (may fail for other reasons on this bare fixture,
	// but never with ErrSealed): a read-only verify and an index rebuild.
	if _, err := Verify(dir, VerifyOptions{}, disc, nil); errors.Is(err, seal.ErrSealed) {
		t.Fatal("a read-only verify of a paused archive must be allowed")
	}
	if _, err := Rebuild(dir, disc); errors.Is(err, seal.ErrSealed) {
		t.Fatal("an index rebuild of a paused archive must be allowed (regenerates derived data)")
	}

	// Positive twin: once resumed, the export is no longer refused by the seal.
	if err := seal.Unseal(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, Options{Out: dir, Inputs: []string{t.TempDir()}}, disc, nil); errors.Is(err, seal.ErrSealed) {
		t.Fatal("after Resume the export must not be refused by the seal")
	}
}
