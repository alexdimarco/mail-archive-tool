package state

import (
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-09
func TestKey(t *testing.T) {
	k := Key("Inbox/Projects", "mid:<abc@example.com>")
	if k != "Inbox/Projects\x00mid:<abc@example.com>" {
		t.Errorf("unexpected key: %q", k)
	}
	// Same identity in different folders yields different keys.
	if Key("A", "id") == Key("B", "id") {
		t.Error("keys should be folder-scoped")
	}
}

// covers: MA-10
func TestLoadMissing(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if m.Len() != 0 {
		t.Errorf("expected empty manifest, got %d", m.Len())
	}
}

// covers: MA-11
func TestAddHasSaveReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "manifest.json")

	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	key := Key("Inbox", "mid:<1@x>")
	if m.Has(key) {
		t.Fatal("unexpected hit on empty manifest")
	}
	rec := Record{Path: "store/Inbox/x.html", Folder: "Inbox", ExportedAt: time.Now().UTC().Truncate(time.Second)}
	m.Add(key, rec)
	if err := m.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.Has(key) {
		t.Fatal("reloaded manifest missing key")
	}
	if reloaded.Len() != 1 {
		t.Errorf("expected 1 entry, got %d", reloaded.Len())
	}
	got := reloaded.Entries[key]
	if got.Path != rec.Path || got.Folder != rec.Folder || !got.ExportedAt.Equal(rec.ExportedAt) {
		t.Errorf("record round-trip mismatch: %+v vs %+v", got, rec)
	}
}
