package desktop

import (
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/wizard"
)

func anyItem(*pendingItem) bool { return true }

// covers: MA-807, R37
// The store of prepared key changes (the P3-3 plan, 3e): every item gets its own
// token, so two archives' changes never meet; an item is used once; a second one for
// the same archive replaces the first; a lock of one archive discards only its own.
func TestPreparedKeyChangesAreSingleUse(t *testing.T) {
	p := newPendingSecrets()
	a := p.put(&pendingItem{key: "a"})
	b := p.put(&pendingItem{key: "b"})
	if a == b || len(a) != 32 || p.get(a, anyItem) == nil || p.get(b, anyItem) == nil {
		t.Fatalf("tokens %q %q", a, b)
	}
	if p.take(a, anyItem) == nil || p.take(a, anyItem) != nil {
		t.Fatal("an item is used once")
	}
	b2 := p.put(&pendingItem{key: "b"})
	if p.get(b, anyItem) != nil || p.get(b2, anyItem) == nil {
		t.Fatal("a second one for the same archive replaces the first")
	}
	c := p.put(&pendingItem{key: "c"})
	p.discardFor("b")
	assure.Reached(t, p.get(b2, anyItem) == nil && p.get(c, anyItem) != nil, "a lock discards only its own archive's")
}

// covers: MA-816, R37
// The store takes an item only as the kind an endpoint expects: a forget with a key
// change's token leaves it waiting, and the reverse too (the P3-3 pass, HTTP-03); an
// item is written to only while it still waits (KEY-07); a switch ends the items
// prepared on another archive's card, never a new archive being made or a forget
// shown (KEY-05); after Shut down nothing is kept, an item put then discarded at
// once; and an item's ten minutes end it (FID-06).
func TestPreparedItemsKeepTheirKindAndLifetimes(t *testing.T) {
	p := newPendingSecrets()
	change := &pendingItem{key: "a", rec: &keyring.PendingRecovery{}}
	tok := p.put(change)
	if p.take(tok, isForgetPlan) != nil || p.get(tok, isKeyChange) != change {
		t.Fatal("a forget took a key change's token, or the key change is gone")
	}
	plan := &pendingItem{key: "p", plan: &keyring.ForgetPlan{}}
	ptok := p.put(plan)
	if p.get(ptok, isKeyChange) != nil || p.take(ptok, isKeyChange) != nil || p.get(ptok, isForgetPlan) != plan {
		t.Fatal("a key change's endpoint took a forget's token")
	}

	ran := false
	if !p.withItem(tok, change, func() { ran = true }) || !ran {
		t.Fatal("positive twin: an item still waiting is written to")
	}
	p.take(tok, anyItem)
	ran = false
	if p.withItem(tok, change, func() { ran = true }) || ran {
		t.Fatal("an item no longer waiting was written to")
	}

	res := &pendingItem{key: "old", res: &keyring.PendingRestore{}}
	rtok := p.put(res)
	making := &pendingItem{key: "new-archive", add: &wizard.PendingAdd{}}
	mtok := p.put(making)
	p.discardExcept("current")
	if p.get(rtok, anyItem) != nil {
		t.Fatal("a switch kept a key file prepared on another archive's card")
	}
	if p.get(mtok, anyItem) == nil || p.get(ptok, anyItem) == nil {
		t.Fatal("a switch ended a new archive being made, or a forget shown")
	}

	short := newPendingSecrets()
	short.life = 20 * time.Millisecond
	stok := short.put(&pendingItem{key: "s"})
	deadline := time.Now().Add(5 * time.Second)
	for short.get(stok, anyItem) != nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	assure.Reached(t, short.get(stok, anyItem) == nil, "an item's lifetime ends it")

	p.close()
	if p.get(mtok, anyItem) != nil || p.get(ptok, anyItem) != nil {
		t.Fatal("Shut down kept an item")
	}
	if late := p.put(&pendingItem{key: "late"}); late != "" {
		t.Fatalf("an item put after Shut down was kept under %q", late)
	}
}
