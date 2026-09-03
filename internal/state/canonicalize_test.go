package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// covers: MA-149, R2, S30
// The store source id is canonicalized so different spellings of one physical
// source collapse to one id (and so one token), instead of minting a duplicate
// tree. PathSourceID is equal for a trailing-separator spelling, a "/./"
// spelling, and (where supported) a symlink to the same target; MailboxSourceID
// lower-cases and trims. A manifest keyed by raw spellings is migrated to the
// canonical id on Load, and two spellings that resolve to one id collapse onto
// the token that owns the plain (hashless) segment.
func TestSourceIDCanonicalizationAndMigration(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "store")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	base := PathSourceID(target)
	if !strings.HasPrefix(base, "path:") {
		t.Errorf("path id lacks the path: prefix: %q", base)
	}
	if got := PathSourceID(target + string(os.PathSeparator)); got != base {
		t.Errorf("trailing-separator spelling gives a different id: %q vs %q", got, base)
	}
	if got := PathSourceID(filepath.Join(dir, ".", "store")); got != base {
		t.Errorf("dot-cluttered spelling gives a different id: %q vs %q", got, base)
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(dir, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if got := PathSourceID(link); got != base {
			t.Errorf("symlink spelling gives a different id: %q vs %q", got, base)
		}
	}
	if got := MailboxSourceID("  User@Contoso.COM "); got != "mailbox:user@contoso.com" {
		t.Errorf("mailbox id not lower-cased/trimmed: %q", got)
	}

	// A manifest keyed by two raw spellings of one source (a plain token and a
	// hashed one) migrates to a single canonical id, keeping the plain token.
	mp := filepath.Join(t.TempDir(), "m.json")
	doc := struct {
		Version int               `json:"version"`
		Entries map[string]Record `json:"entries"`
		Stores  map[string]string `json:"stores"`
	}{Version: 3, Entries: map[string]Record{}, Stores: map[string]string{
		target:                            "store",          // plain-segment token
		target + string(os.PathSeparator): "store~deadbeef", // a second raw spelling, hashed token
	}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mp, data, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(mp)
	if err != nil {
		t.Fatal(err)
	}
	if m.StoresMigrated == 0 {
		t.Error("raw-spelling stores map was not migrated (StoresMigrated=0)")
	}
	if len(m.Stores) != 1 {
		t.Errorf("two spellings of one source did not collapse: stores=%v", m.Stores)
	}
	if tok := m.Stores[base]; tok != "store" {
		t.Errorf("collapsed token = %q, want the plain segment %q (stores=%v)", tok, "store", m.Stores)
	}
}

// covers: MA-150, R4, S30
// A tampered store token in the manifest cannot become an on-disk directory:
// Load drops any stores-map token that is not a single safe path segment, and
// Token re-derives a fresh, in-root token when the stored one is unsafe — so no
// consumer ever sees a traversing token (INS2-1).
func TestTamperedStoreTokenSanitized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tampered.json")
	doc := struct {
		Version int               `json:"version"`
		Entries map[string]Record `json:"entries"`
		Stores  map[string]string `json:"stores"`
	}{Version: 3, Entries: map[string]Record{}, Stores: map[string]string{
		"path:/a": "../escape",
		"path:/b": "good",
	}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for id, tok := range m.Stores {
		if !SafeToken(tok) {
			t.Errorf("Load kept an unsafe token %q for %q", tok, id)
		}
	}
	if _, bad := m.Stores["path:/a"]; bad {
		t.Errorf("the tampered token was not dropped: %v", m.Stores)
	}
	if m.Stores["path:/b"] != "good" {
		t.Errorf("a clean token was lost: %v", m.Stores)
	}
	// Token re-derives a safe token even when the stored one is unsafe.
	m.Stores["path:/c"] = "../../evil"
	if tok := m.Token("path:/c", "Display Name"); !SafeToken(tok) {
		t.Errorf("Token returned an unsafe, traversing token %q", tok)
	}
}

// covers: MA-152, R8, R2, S30
// Self-heal after an old-binary excursion keeps the CANONICAL file as the record
// of truth. When a re-scoped legacy (v2) key collides with a surviving v3 key,
// the pre-existing survivor's record — pointing at the canonical file — is kept
// and the legacy entry — pointing at the excursion's duplicate copy — is dropped,
// so `verify` checks the canonical file and never flags it as unexpected while
// trusting the duplicate (Friction #5).
func TestSelfHealKeepsCanonicalRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "excursion.json")
	writeManifestJSON(t, path, 2, map[string]legacyRec{
		// survivor: the upgraded binary's v3 record at the canonical path.
		"s" + keySeparator + "Inbox" + keySeparator + "mid:<m@x>": {Path: "s/Inbox/2025-01-01_canonical_aaaa.html", Folder: "Inbox"},
		// excursion: the old binary's v2 record at a DIFFERENT (duplicate) path.
		"Inbox" + keySeparator + "mid:<m@x>": {Path: "s/Inbox/2025-01-01_duplicate_bbbb.html", Folder: "Inbox"},
	})
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Len() != 1 {
		t.Fatalf("collision not deduped: %d entries", m.Len())
	}
	rec, ok := m.Get(Key("s", "Inbox", "mid:<m@x>"))
	if !ok {
		t.Fatal("survivor record missing after self-heal")
	}
	if rec.Path != "s/Inbox/2025-01-01_canonical_aaaa.html" {
		t.Errorf("self-heal kept the excursion duplicate, not the canonical file: %q", rec.Path)
	}
	for k, r := range m.Entries {
		if r.Path == "s/Inbox/2025-01-01_duplicate_bbbb.html" {
			t.Errorf("a record still points at the excursion duplicate: key=%q", k)
		}
	}
}
