package state

import (
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-09, R6
func TestKey(t *testing.T) {
	k := Key("Local Folders", "Inbox/Projects", "mid:<abc@example.com>")
	if k != "Local Folders\x00Inbox/Projects\x00mid:<abc@example.com>" {
		t.Errorf("unexpected key: %q", k)
	}
	// Same identity in different folders yields different keys.
	if Key("s", "A", "id") == Key("s", "B", "id") {
		t.Error("keys should be folder-scoped")
	}
	// Same folder + identity in different stores yields different keys, so two
	// mailboxes archived into one -out never collide (R6).
	if Key("s1", "A", "id") == Key("s2", "A", "id") {
		t.Error("keys should be store-scoped")
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

// covers: MA-44, R13
// Delete removes an entry (so a later Has is false and it survives a save/reload
// round-trip); deleting an absent key is a no-op. This is what reindex uses to
// prune a manifest entry whose exported file is gone.
func TestManifestDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	keep := Key("s", "Inbox", "id-keep")
	drop := Key("s", "Inbox", "id-drop")
	m.Add(keep, Record{Path: "store/Inbox/keep.html"})
	m.Add(drop, Record{Path: "store/Inbox/drop.html"})

	m.Delete(drop)
	m.Delete("never-existed") // no-op, must not panic or affect others

	if m.Has(drop) {
		t.Error("deleted key still present")
	}
	if !m.Has(keep) {
		t.Error("Delete removed the wrong key")
	}
	if m.Len() != 1 {
		t.Errorf("len after delete = %d, want 1", m.Len())
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Has(drop) || !reloaded.Has(keep) {
		t.Errorf("delete did not persist: has(drop)=%v has(keep)=%v", reloaded.Has(drop), reloaded.Has(keep))
	}
}

// covers: MA-11
func TestAddHasSaveReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "manifest.json")

	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	key := Key("s", "Inbox", "mid:<1@x>")
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
