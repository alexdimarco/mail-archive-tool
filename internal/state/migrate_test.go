package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const legacyManifest = `{
  "version": 1,
  "entries": {
    "Inbox\u0000mid:<a@x>": {"path": "s/Inbox/a.html", "folder": "Inbox", "exported_at": "2026-01-01T00:00:00Z"},
    "Sent\u0000mid:<b@x>":  {"path": "s/Sent/b.html",  "folder": "Sent",  "exported_at": "2026-01-01T00:00:00Z"}
  }
}`

// covers: MA-70, R1, R5
// A manifest written before completeness tracking (version 1) cannot know which
// entries are incomplete, so every record is migrated to the "unknown" sentinel
// (fillable — the next incremental run re-examines it) and counted; Save writes
// the current version and a reload keeps the sentinel without migrating again. A
// version-1 file that already holds entries — what an older binary leaves after
// a downgrade — is treated the same way, never as silently complete.
func TestLegacyManifestMigratesToUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	if err := os.WriteFile(path, []byte(legacyManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Migrated != 2 {
		t.Errorf("Migrated = %d, want 2", m.Migrated)
	}
	for key, rec := range m.Entries {
		if strings.Join(rec.Missing, ",") != "unknown" || !rec.Fillable() || rec.Complete() {
			t.Errorf("%q not migrated to the unknown sentinel: %+v", key, rec)
		}
	}
	if f, tm, u := m.Counts(); f != 0 || tm != 0 || u != 2 {
		t.Errorf("Counts = fillable %d terminal %d unknown %d, want 0/0/2", f, tm, u)
	}

	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	// Save writes compact JSON (nas-03), so the version field carries no space.
	if !strings.Contains(string(data), `"version":3`) || !strings.Contains(string(data), `"unknown"`) {
		t.Errorf("saved manifest is not the current version with the sentinel persisted:\n%s", data)
	}
	m2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Migrated != 0 {
		t.Errorf("an already-migrated manifest must not sentinel-migrate again (Migrated=%d)", m2.Migrated)
	}
	if _, _, u := m2.Counts(); u != 2 {
		t.Errorf("sentinel lost on reload: unknown=%d", u)
	}

	// Downgrade: an old binary rewrote the file as version 1 with entries.
	if err := os.WriteFile(path, []byte(legacyManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	m3, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m3.Migrated != 2 {
		t.Errorf("downgraded manifest not re-migrated: Migrated=%d", m3.Migrated)
	}

	// A complete version-2 record stays complete and issue-free.
	m3.Add("k", Record{Path: "p.html", Folder: "F"})
	rec, _ := m3.Get("k")
	if !rec.Complete() || rec.Fillable() {
		t.Errorf("fresh record is not complete: %+v", rec)
	}
	if got := m3.Issues(); len(got) != 2 {
		t.Errorf("Issues() = %d records, want the 2 unknown ones", len(got))
	}
}
