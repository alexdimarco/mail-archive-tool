package app

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: MA-232, R13, S30, S39
// The manifest is untrusted, so reindex redaction must NOT feed a Path/AlsoFiles
// entry to os.Remove without a path gate: a tampered `../` entry could delete a
// file OUTSIDE the archive. validRelPath rejects it; the deletion is skipped.
func TestReindexRedactionRefusesTraversalAlsoFile(t *testing.T) {
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)

	// A victim file OUTSIDE -out (its own auto-cleaned tempdir), targeted by a
	// `../…` AlsoFiles traversal computed relative to -out.
	victimDir := t.TempDir()
	victim := filepath.Join(victimDir, "victim.html")
	if err := os.WriteFile(victim, []byte("do not delete"), 0o644); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(out, victim)
	if err != nil {
		t.Fatal(err)
	}
	traversal := filepath.ToSlash(rel) // e.g. ../victimDir/victim.html

	survivor := filepath.ToSlash(filepath.Join("store", "Inbox", "m.html"))
	sp := filepath.Join(out, filepath.FromSlash(survivor))
	_ = os.MkdirAll(filepath.Dir(sp), 0o755)
	if err := os.WriteFile(sp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	key := state.LiveKey("store", "mid:m@x")
	doc := struct {
		Version int                     `json:"version"`
		Entries map[string]state.Record `json:"entries"`
	}{Version: 5, Entries: map[string]state.Record{
		key: {Path: survivor, Folder: "Inbox", FirstFolder: "Inbox", Present: true,
			Fingerprint: "aaaaaaaaaaaaaaaa", FpScheme: state.FpSchemeCurrent, AlsoFiles: []string{traversal}},
	}}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.Add("store", []string{"Inbox"}, &model.Message{Subject: "m", InternetMessageID: "<m@x>"}, survivor, key); err != nil {
		t.Fatal(err)
	}
	_ = ix.Flush()
	ix.Close()

	// Redact the survivor, then reindex — the `..` AlsoFiles entry must be refused.
	if err := os.Remove(sp); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Reindex(out, logger); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	if _, err := os.Stat(victim); err != nil {
		t.Errorf("redaction followed a `../` AlsoFiles entry and deleted a file OUTSIDE the archive (%v)", err)
	}
}
