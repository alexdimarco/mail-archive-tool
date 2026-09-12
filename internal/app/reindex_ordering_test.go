package app

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-220, R21, R5, S37
// reindex persists the reconciled manifest — the keep-authority for history
// compaction — BEFORE it compacts the log, so a compaction failure (or a crash
// between the two) still durably records the redaction; the derived log heals
// from (manifest ∩ disk) on the next reindex, and the manifest never leads the
// log (#11, adversarial 2026-09-12; §3.5's ordering for the redaction path).
// Fault injection: a directory at the history path makes CompactHistory fail
// (ReadHistory cannot read a directory); the manifest must still be saved.
func TestReindexSavesManifestBeforeCompactingHistory(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true}, logger, nil); err != nil {
		t.Fatal(err)
	}

	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	m0, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	var victimKey, victimPath string
	for k, r := range m0.All() {
		victimKey, victimPath = k, r.Path
		break
	}
	if victimKey == "" {
		t.Fatal("no archived message to redact")
	}
	// Redact: delete the message's file from disk.
	if err := os.Remove(filepath.Join(out, filepath.FromSlash(victimPath))); err != nil {
		t.Fatal(err)
	}
	// Force CompactHistory to fail: put a directory where the log would be.
	hp := filepath.Join(out, state.HistoryName)
	_ = os.Remove(hp)
	if err := os.Mkdir(hp, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, _, rerr := Reindex(out, logger); rerr == nil {
		t.Fatalf("Reindex did not surface the history-compaction failure (the injection did not bite)")
	}

	// Despite the compaction failure, the redaction is durable: the manifest was
	// saved before the log was compacted. Prove-fail: the old order compacts
	// before saving, so it returns on the failure with the victim still recorded.
	m1, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m1.Get(victimKey); ok {
		t.Errorf("redacted message %q is still in the manifest after a compaction failure — the manifest was not saved before history compaction (#11)", victimKey)
	}
}
