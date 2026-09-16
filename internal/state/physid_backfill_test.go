package state

import (
	"path/filepath"
	"testing"
)

// covers: MA-243, R17, S38
// HC10 + rev-6.2 id-set: the mailbox-wide identity index carries a record's primary
// PhysID, and AddPhysID records content-equal immutable ids — the FIRST becomes the
// primary (and is indexed in byIdent), further ones append to AltPhysIDs — so ONE
// record represents several simultaneously-live copies of a message (same
// Message-ID, identical content, distinct Graph ids) without flipping a single slot.
// HasPhysID matches any id in the set. No-ops (empty id, unknown key, already-known)
// report false. Never touches identity or fingerprint.
func TestAddPhysIDAndHasPhysID(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), "m.json"))
	if err != nil {
		t.Fatal(err)
	}

	// A record captured with a primary id, indexed by identity.
	key := LiveKey("store", "mid:<p@x>")
	m.Add(key, Record{Path: "store/Inbox/p.html", Fingerprint: "ffffffffffffffff", PhysID: "IMM-1"})
	if refs := m.KeysForIdentity("mid:<p@x>"); len(refs) != 1 || refs[0].PhysID != "IMM-1" {
		t.Fatalf("index PhysID = %+v, want IMM-1 (primary indexed on Add)", refs)
	}

	// A second content-equal id (a copy in another folder): appended, both match.
	if !m.AddPhysID(key, "IMM-2") {
		t.Fatal("AddPhysID reported no change for a new content-equal id")
	}
	if !m.HasPhysID(key, "IMM-1") || !m.HasPhysID(key, "IMM-2") {
		t.Errorf("HasPhysID must match BOTH the primary and the added id")
	}
	if r, _ := m.Get(key); r.PhysID != "IMM-1" || len(r.AltPhysIDs) != 1 || r.AltPhysIDs[0] != "IMM-2" {
		t.Errorf("record = PhysID %q Alt %v, want IMM-1 + [IMM-2] (primary not flipped)", r.PhysID, r.AltPhysIDs)
	}

	// A record with NO primary id yet: AddPhysID sets the primary AND indexes it.
	empty := LiveKey("store", "mid:<q@x>")
	m.Add(empty, Record{Path: "store/Inbox/q.html", Fingerprint: "eeeeeeeeeeeeeeee"})
	if !m.AddPhysID(empty, "IMM-Q") {
		t.Fatal("AddPhysID reported no change on an empty-primary record")
	}
	if r, _ := m.Get(empty); r.PhysID != "IMM-Q" || len(r.AltPhysIDs) != 0 {
		t.Errorf("empty-primary record = PhysID %q Alt %v, want IMM-Q as the primary", r.PhysID, r.AltPhysIDs)
	}
	if refs := m.KeysForIdentity("mid:<q@x>"); len(refs) != 1 || refs[0].PhysID != "IMM-Q" {
		t.Errorf("index PhysID = %+v, want IMM-Q indexed as the primary", refs)
	}

	// No-ops report false and change nothing.
	if m.AddPhysID(key, "IMM-1") || m.AddPhysID(key, "IMM-2") {
		t.Errorf("AddPhysID reported a change for an already-known id")
	}
	if m.AddPhysID(key, "") {
		t.Errorf("AddPhysID reported a change for an empty id")
	}
	if m.AddPhysID(LiveKey("store", "mid:<absent@x>"), "IMM-9") {
		t.Errorf("AddPhysID reported a change for an unknown key")
	}
	if m.HasPhysID(key, "") || m.HasPhysID(LiveKey("store", "mid:<absent@x>"), "IMM-1") {
		t.Errorf("HasPhysID must be false for an empty id or an unknown key")
	}
}
