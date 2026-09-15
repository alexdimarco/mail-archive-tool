package state

import (
	"path/filepath"
	"testing"
)

// covers: MA-243, R17, S38
// HC10 index wiring: the mailbox-wide identity index carries each record's PhysID
// (the fast-path compares immutable ids without a per-sibling Get), and
// BackfillPhys stamps a PhysID onto an existing record AND its identity-index entry
// under one lock — the survivor/empty-PhysID adopt path (rev-6 §5.4/§6). A no-op
// (unknown key, empty id, or the same id already stored) reports false and changes
// nothing. It never touches identity or fingerprint.
func TestByIdentPhysIDAndBackfill(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), "m.json"))
	if err != nil {
		t.Fatal(err)
	}
	key := LiveKey("store", "mid:<p@x>")
	m.Add(key, Record{Path: "store/Inbox/p.html", Fingerprint: "ffffffffffffffff", PhysID: "IMM-1"})

	refs := m.KeysForIdentity("mid:<p@x>")
	if len(refs) != 1 || refs[0].PhysID != "IMM-1" {
		t.Fatalf("index PhysID = %+v, want IMM-1 carried into byIdent on Add", refs)
	}

	// Backfill a DIFFERENT record that has no PhysID yet (a pre-v6 / withheld sibling).
	empty := LiveKey("store", "mid:<q@x>")
	m.Add(empty, Record{Path: "store/Inbox/q.html", Fingerprint: "eeeeeeeeeeeeeeee"})
	if !m.BackfillPhys(empty, "IMM-2") {
		t.Fatalf("BackfillPhys reported no change on a record that had no PhysID")
	}
	if r, _ := m.Get(empty); r.PhysID != "IMM-2" {
		t.Errorf("Entries PhysID = %q, want IMM-2 after backfill", r.PhysID)
	}
	if refs := m.KeysForIdentity("mid:<q@x>"); len(refs) != 1 || refs[0].PhysID != "IMM-2" {
		t.Errorf("index PhysID = %+v, want IMM-2 after backfill (index refreshed under the same lock)", refs)
	}

	// No-ops: same id already stored, empty id, unknown key.
	if m.BackfillPhys(empty, "IMM-2") {
		t.Errorf("BackfillPhys reported a change when the PhysID was already IMM-2")
	}
	if m.BackfillPhys(empty, "") {
		t.Errorf("BackfillPhys reported a change for an empty PhysID")
	}
	if m.BackfillPhys(LiveKey("store", "mid:<absent@x>"), "IMM-9") {
		t.Errorf("BackfillPhys reported a change for an unknown key")
	}
}
