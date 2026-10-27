package desktop

import (
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-807, R37
// The store of prepared key changes (the P3-3 plan, 3e): every item gets its own
// token, so two archives' changes never meet; an item is used once; a second one for
// the same archive replaces the first; a lock of one archive discards only its own.
func TestPreparedKeyChangesAreSingleUse(t *testing.T) {
	p := newPendingSecrets()
	a := p.put(&pendingItem{key: "a"})
	b := p.put(&pendingItem{key: "b"})
	if a == b || len(a) != 32 || p.get(a) == nil || p.get(b) == nil {
		t.Fatalf("tokens %q %q", a, b)
	}
	if p.take(a) == nil || p.take(a) != nil {
		t.Fatal("an item is used once")
	}
	b2 := p.put(&pendingItem{key: "b"})
	if p.get(b) != nil || p.get(b2) == nil {
		t.Fatal("a second one for the same archive replaces the first")
	}
	c := p.put(&pendingItem{key: "c"})
	p.discardFor("b")
	assure.Reached(t, p.get(b2) == nil && p.get(c) != nil, "a lock discards only its own archive's")
}
