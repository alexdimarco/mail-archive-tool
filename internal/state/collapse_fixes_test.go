package state

import (
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-234, R3, R17, S39
// The collapse discriminates a v3 folder-scoped key by the identity at parts[2],
// tested BEFORE the LiveKey (parts[1]) case — so a v3 key whose FOLDER is
// literally named "mid:…"/"sha:…" is still migrated, not mis-read as an
// already-collapsed LiveKey and left un-migrated (which would re-download +
// duplicate + phantom-gone it on the next live run).
func TestCollapseMigratesV3KeyWithIdentityShapedFolder(t *testing.T) {
	t1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	k := Key("mbox", "mid:2024", "mid:<x@x>") // folder literally named "mid:2024"
	path := filepath.Join(t.TempDir(), "v3.json")
	writeFullManifest(t, path, 3, map[string]Record{
		k: {Path: "mbox/mid:2024/x.html", Folder: "mid:2024", ExportedAt: t1, Fingerprint: "ffffffffffffffff"},
	})
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	_, remap := m.CollapseByIdentity(nil, "mbox")
	base := LiveKey("mbox", "mid:<x@x>")
	if _, ok := m.Get(base); !ok {
		t.Errorf("a v3 key with a mid:-named folder was NOT migrated (mis-classified as an already-collapsed LiveKey)")
	}
	if _, ok := m.Get(k); ok {
		t.Errorf("the v3 folder-scoped key survived verbatim — it was not migrated")
	}
	if remap[k] != base {
		t.Errorf("remap[%q] = %q, want %q", k, remap[k], base)
	}
}

// covers: MA-235, R1, R3, S39
// Collapse merges same-(token,identity) records ONLY when their on-disk files are
// byte-identical (a real move-duplicate). Two DISTINCT messages that reused one
// Message-ID with an identical envelope hash to the SAME fingerprint (bodies are
// excluded); with differing files (sameContent=false) they must both survive as
// distinct records — merging would drop one from the manifest and index (R1).
func TestCollapseKeepsDistinctSameEnvelopeReuses(t *testing.T) {
	t1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	k1 := Key("mbox", "Inbox", "mid:<dup@x>")
	k2 := Key("mbox", "Sent", "mid:<dup@x>")
	path := filepath.Join(t.TempDir(), "v3.json")
	writeFullManifest(t, path, 3, map[string]Record{
		k1: {Path: "mbox/Inbox/a.html", Folder: "Inbox", ExportedAt: t1, Fingerprint: "eeeeeeeeeeeeeeee"},
		k2: {Path: "mbox/Sent/b.html", Folder: "Sent", ExportedAt: t1.Add(time.Hour), Fingerprint: "eeeeeeeeeeeeeeee"}, // SAME fp
	})
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// Files differ (distinct bodies).
	losses, _ := m.CollapseByIdentity(func(a, b string) bool { return false }, "mbox")
	if len(losses) != 0 {
		t.Errorf("a distinct same-envelope reuse was merged away (%d losses) — R1 drop", len(losses))
	}
	if n := len(m.KeysForIdentity("mid:<dup@x>")); n != 2 {
		t.Errorf("identity has %d records after collapse, want 2 (both distinct reuses kept)", n)
	}
}
