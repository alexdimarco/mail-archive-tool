package state

import (
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-224, R1, R3, S39
// The collapse discriminates key SHAPE by which component is the identity, not by
// NUL/component count (EC4): a healthy #fp-qualified LiveKey
// (token\x00identity\x00fp) has three components exactly like a v3 folder-scoped
// key (token\x00folder\x00identity) but MUST survive verbatim — collapsing it
// would merge or drop a distinct id-reuser (a regression). And the collapse is
// SCOPED to the token(s) passed: a mailbox not archived this run keeps its keys
// untouched (R3), and is collapsed only when it is itself archived later.
func TestCollapseDiscriminatesAndScopes(t *testing.T) {
	t1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	baseA := LiveKey("mbox", "mid:<a@x>")       // bare LiveKey
	qualA := Qualify(baseA, "abcdef0123456789") // #fp-qualified LiveKey (3 parts)
	liveB := LiveKey("mbox", "mid:<b@x>")       // another bare LiveKey
	o1 := Key("other", "Inbox", "mid:<c@x>")    // a v3 move-duplicate of a
	o2 := Key("other", "Trash", "mid:<c@x>")    // DIFFERENT token (out of scope)

	path := filepath.Join(t.TempDir(), "m.json")
	writeFullManifest(t, path, 5, map[string]Record{
		baseA: {Path: "mbox/Inbox/a.html", Folder: "Inbox", ExportedAt: t1, Fingerprint: "1111111111111111", FpScheme: FpSchemeCurrent, FirstFolder: "Inbox", FirstSeen: t1, LastSeen: t1, Present: true},
		qualA: {Path: "mbox/Sent/a2.html", Folder: "Sent", ExportedAt: t1, Fingerprint: "abcdef0123456789", FpScheme: FpSchemeCurrent, FirstFolder: "Sent", FirstSeen: t1, LastSeen: t1, Present: true},
		liveB: {Path: "mbox/Inbox/b.html", Folder: "Inbox", ExportedAt: t1, Fingerprint: "2222222222222222", FpScheme: FpSchemeCurrent, FirstFolder: "Inbox", FirstSeen: t1, LastSeen: t1, Present: true},
		o1:    {Path: "other/Inbox/c.html", Folder: "Inbox", ExportedAt: t1, Fingerprint: "3333333333333333"},
		o2:    {Path: "other/Trash/c.html", Folder: "Trash", ExportedAt: t1, Fingerprint: "3333333333333333"},
	})
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	// Scope to mbox: its keys are all already LiveKey-shaped, so NOTHING collapses
	// and the qualified sibling survives verbatim.
	losses, remap := m.CollapseByIdentity(nil, "mbox")
	if len(losses) != 0 || len(remap) != 0 {
		t.Errorf("collapse touched healthy LiveKeys of the in-scope token: losses=%d remap=%d", len(losses), len(remap))
	}
	for _, k := range []string{baseA, qualA, liveB} {
		if _, ok := m.Get(k); !ok {
			t.Errorf("healthy LiveKey %q was corrupted or dropped by collapse (EC4)", k)
		}
	}
	// The out-of-scope token's v3 move-duplicate is left UNTOUCHED.
	if _, ok := m.Get(o1); !ok {
		t.Errorf("out-of-scope token's key %q was collapsed (scope violated, R3)", o1)
	}
	if _, ok := m.Get(o2); !ok {
		t.Errorf("out-of-scope token's key %q was collapsed (scope violated, R3)", o2)
	}

	// Now archive "other": its move-duplicate unifies to one LiveKey, and the
	// remap points the survivor's old folder-scoped key at the new LiveKey.
	lo, re := m.CollapseByIdentity(nil, "other")
	if len(lo) != 1 {
		t.Fatalf("other collapse losses=%d, want 1 (the Trash copy)", len(lo))
	}
	baseC := LiveKey("other", "mid:<c@x>")
	if _, ok := m.Get(baseC); !ok {
		t.Errorf("the other-token move-duplicate did not collapse to its LiveKey %q", baseC)
	}
	if re[o1] != baseC {
		t.Errorf("remap[%q] = %q, want the survivor re-keyed to %q (EC5)", o1, re[o1], baseC)
	}
}
