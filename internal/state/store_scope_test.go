package state

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// legacyRec is the minimal on-disk record shape these fixtures need.
type legacyRec struct {
	Path       string `json:"path"`
	Folder     string `json:"folder,omitempty"`
	ExportedAt string `json:"exported_at,omitempty"`
}

// writeManifestJSON marshals a version + entries map to path. Keys carry real
// NUL separators, which json escapes on disk — exactly the shape Load
// reads back.
func writeManifestJSON(t *testing.T, path string, version int, entries map[string]legacyRec) {
	t.Helper()
	doc := struct {
		Version int                  `json:"version"`
		Entries map[string]legacyRec `json:"entries"`
	}{Version: version, Entries: entries}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-132, R5, R12
// A manifest written by a newer mailarchive (a stored version above what this
// build understands) is refused with a typed error that names the file, the
// versions, and the upgrade remedy — never silently re-interpreted under the
// current format — and the file is left byte-for-byte unchanged (fail-closed).
// Positive twin first: a current-version manifest loads clean.
func TestManifestRefusesNewerVersion(t *testing.T) {
	good := filepath.Join(t.TempDir(), "good.json")
	if err := os.WriteFile(good, []byte(`{"version":4,"entries":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(good); err != nil {
		t.Fatalf("a current-version manifest must load: %v", err)
	}

	path := filepath.Join(t.TempDir(), "future.json")
	body := []byte(`{"version":5,"entries":{"whatever":{"path":"s/Inbox/a.html"}}}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	rc, msg := 0, ""
	if err != nil {
		rc, msg = 2, err.Error()
	}
	assure.Refused(t, rc, msg,
		assure.Names(path, "newer mailarchive", "upgrade"),
		assure.NoSideEffect(func() bool {
			after, readErr := os.ReadFile(path)
			return readErr == nil && bytes.Equal(after, body)
		}))
}

// covers: MA-133, R1, R5, S6
// The unknown-sentinel migration is gated on the LITERAL version 1, decoupled
// from the current version constant (FC3): a complete version-2 record stays
// complete after the v3 re-scope (never re-sentinelled into a fillable
// "unknown", which for a dead-source one-shot import would falsely re-open a
// finished archive). The re-scope itself still fires (the one-NUL key becomes
// store-qualified), and a genuine version-1 record IS still sentinelled — so
// the gate is version-sensitive, not a no-op.
func TestSentinelFiresForVersion1Only(t *testing.T) {
	v2 := filepath.Join(t.TempDir(), "v2.json")
	writeManifestJSON(t, v2, 2, map[string]legacyRec{
		"Inbox" + keySeparator + "mid:<done@x>": {Path: "store/Inbox/done.html", Folder: "Inbox", ExportedAt: "2026-01-01T00:00:00Z"},
	})
	m, err := Load(v2)
	if err != nil {
		t.Fatal(err)
	}
	if m.Migrated != 0 {
		t.Errorf("a version-2 record was re-sentinelled (Migrated=%d); the gate must be the literal version 1", m.Migrated)
	}
	if m.Version != manifestVersion {
		t.Errorf("load did not upgrade the version to %d: got %d", manifestVersion, m.Version)
	}
	if m.Rekeyed != 1 {
		t.Errorf("the one-NUL v2 key was not re-scoped (Rekeyed=%d, want 1)", m.Rekeyed)
	}
	rec, ok := m.Get(Key("store", "Inbox", "mid:<done@x>"))
	if !ok {
		t.Fatal("re-scoped record not found under its v3 (store-qualified) key")
	}
	if !rec.Complete() || rec.Fillable() || rec.Unknown() {
		t.Errorf("a complete v2 record did not stay complete after the v3 load: %+v", rec)
	}

	v1 := filepath.Join(t.TempDir(), "v1.json")
	writeManifestJSON(t, v1, 1, map[string]legacyRec{
		"Inbox" + keySeparator + "mid:<old@x>": {Path: "store/Inbox/old.html", Folder: "Inbox", ExportedAt: "2026-01-01T00:00:00Z"},
	})
	m1, err := Load(v1)
	if err != nil {
		t.Fatal(err)
	}
	if m1.Migrated != 1 {
		t.Errorf("a version-1 record was not sentinelled (Migrated=%d, want 1)", m1.Migrated)
	}
	if r, _ := m1.Get(Key("store", "Inbox", "mid:<old@x>")); !r.Unknown() {
		t.Errorf("the version-1 record did not carry the unknown sentinel after re-scope: %+v", r)
	}
}

// covers: MA-131, R5, R8, S30
// A manifest holding a MIX of one-NUL (legacy v2) and two-NUL (current v3) keys
// — what an already-shipped binary leaves after a single write against an
// upgraded archive — is repaired by content on the next load: every one-NUL key
// is re-scoped from its record's own path, every two-NUL key is left exactly as
// it is (never double-prefixed into store\x00store\x00…), and when a re-scoped
// key collides with a surviving v3 key the PRE-EXISTING survivor is kept (its
// canonical path) and the legacy entry dropped, so exactly one v3 entry remains
// per message and verify keeps pointing at the canonical file rather than the
// excursion's duplicate (Friction #5/R8). (The index half is proven in the index
// package.)
func TestMixedKeysRepairedWithoutDoublePrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mixed.json")
	writeManifestJSON(t, path, 2, map[string]legacyRec{
		"s" + keySeparator + "Inbox" + keySeparator + "mid:<a@x>": {Path: "s/Inbox/a.html", Folder: "Inbox"},     // v3, untouched
		"Sent" + keySeparator + "mid:<b@x>":                       {Path: "s/Sent/b.html", Folder: "Sent"},       // v2, re-scoped
		"s" + keySeparator + "Draft" + keySeparator + "mid:<c@x>": {Path: "s/Draft/c-new.html", Folder: "Draft"}, // v3, collision survivor-to-be
		"Draft" + keySeparator + "mid:<c@x>":                      {Path: "s/Draft/c-old.html", Folder: "Draft"}, // v2, collides after re-scope
	})
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Rekeyed != 2 {
		t.Errorf("Rekeyed = %d, want 2 (only the two one-NUL keys)", m.Rekeyed)
	}
	if m.Len() != 3 {
		t.Errorf("entries after repair = %d, want 3 (the collision deduped)", m.Len())
	}
	for k := range m.Entries {
		if n := strings.Count(k, keySeparator); n != 2 {
			t.Errorf("key %q holds %d NUL separators, want 2 (a v3 key)", k, n)
		}
		if strings.HasPrefix(k, "s"+keySeparator+"s"+keySeparator) {
			t.Errorf("key %q was double-prefixed", k)
		}
	}
	if r, ok := m.Get(Key("s", "Inbox", "mid:<a@x>")); !ok || r.Path != "s/Inbox/a.html" {
		t.Errorf("untouched v3 entry lost or altered: ok=%v rec=%+v", ok, r)
	}
	if r, ok := m.Get(Key("s", "Sent", "mid:<b@x>")); !ok || r.Path != "s/Sent/b.html" {
		t.Errorf("re-scoped v2 entry missing: ok=%v rec=%+v", ok, r)
	}
	if r, ok := m.Get(Key("s", "Draft", "mid:<c@x>")); !ok || r.Path != "s/Draft/c-new.html" {
		t.Errorf("collision must keep the pre-existing v3 survivor's canonical path, not the re-scoped legacy duplicate (Friction #5): ok=%v rec=%+v", ok, r)
	}
}
