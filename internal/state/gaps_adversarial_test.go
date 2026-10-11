package state

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The state-level tombstones of the FI-S3 adversarial pass
// (docs/review-fault-isolation-s3-adversarial.md).

// covers: MA-323, R1, R5, S46
// S3-AO9 / S3-AI6: BoundLocator is the locator a lookup builds a ledger key from when
// the text is the source's own. The entry ObserveGap stores for a hostile id is found
// under the key built from the same raw text (the ledger strips control and bidi
// characters and cuts at 512 bytes, so a key built from the raw text found nothing),
// and ids the ledger would have merged (a shared 512-byte prefix, or a difference
// only in stripped characters) keep an entry each. A clean locator is returned as it is.
func TestBoundLocatorKeysEveryRawLocatorToItsOwnEntry(t *testing.T) {
	m, _, tok := gapManifest(t)
	long := "graph:" + strings.Repeat("a", 600)
	raws := []string{
		"graph:plain-id",
		long + "1", long + "2",
		"graph:C\x011", "graph:C\x021", "graph:C1",
		"graph:A‮B", "graph:AB",
	}
	if got := BoundLocator("graph:plain-id"); got != "graph:plain-id" {
		t.Errorf("a clean locator was changed: %q", got)
	}
	seen := map[string]string{}
	for _, raw := range raws {
		loc := BoundLocator(raw)
		if loc != boundString(loc, maxGapLocator) {
			t.Errorf("BoundLocator(%q) = %q, which the ledger would clean again: a lookup built from it would miss", raw, loc)
		}
		if prev, dup := seen[loc]; dup && prev != raw {
			t.Errorf("%q and %q share the locator %q: they would share one ledger entry", prev, raw, loc)
		}
		seen[loc] = raw
		key, created := m.ObserveGap(itemGap(tok, loc), gapBase)
		if !created || key != GapKey(tok, GapScopeItem, loc) {
			t.Errorf("the entry for %q is stored under %q (created %v), not the key a lookup builds: %q", raw, key, created, GapKey(tok, GapScopeItem, loc))
		}
		if _, ok := m.GapFor(GapKey(tok, GapScopeItem, BoundLocator(raw))); !ok {
			t.Errorf("no entry under the key built from %q", raw)
		}
	}
	if got := len(m.CaptureGapsSorted()); got != len(raws) {
		t.Errorf("the ledger holds %d entries for %d distinct ids", got, len(raws))
	}
	if m.MissingGapCount() != len(raws) {
		t.Errorf("the ledger counts %d items missing, want %d", m.MissingGapCount(), len(raws))
	}
}

// legacyIdentityFor stands for model.Message{Subject, Received}.Identity() here: the
// state package does not import the model, and the test only needs a function of the
// received time.
func legacyIdentityFor(t time.Time) string {
	return fmt.Sprintf("sha:legacy-%d", t.UnixNano())
}

// covers: MA-323, R1, R5, S46
// S3-AI8 / S3-AI11: AdoptLegacyGraphStubs adopts a record only when its identity is
// the stub identity of this message's received time (a real message that looks like a
// stub is no match), finds the candidates through the identity index (a lookup per
// unparseable message, not a scan of the archive), and still works when the archive
// is large: 500 adoptions over a 100,000-record manifest take milliseconds, where the
// scan took about 4 ms each (two seconds in all) and grew with the archive.
func TestAdoptLegacyGraphStubsMatchesTheStubIdentityThroughTheIndex(t *testing.T) {
	m, _, tok := gapManifest(t)
	received := time.Date(2025, 2, 1, 10, 0, 0, 0, time.UTC)
	old := gapBase.Add(-48 * time.Hour)
	stub := func(name string) Record {
		return Record{Path: tok + "/Inbox/" + name + ".html", Folder: "Inbox", FirstFolder: "Inbox", Present: true, LastSeen: old,
			Subject: LegacyPlaceholderSubject, Date: received.Format(time.RFC3339), Terminal: []string{MissingBody}}
	}
	kStub := LiveKey(tok, legacyIdentityFor(received))
	kLook := LiveKey(tok, "sha:a-real-message-that-looks-like-a-stub")
	m.Add(kStub, stub("stub"))
	m.Add(kLook, stub("look"))
	if n := m.AdoptLegacyGraphStubs(tok, "", received, gapBase, legacyIdentityFor); n != 1 {
		t.Fatalf("adopted %d records, want only the one whose identity is the stub's", n)
	}
	if r, _ := m.Get(kStub); len(r.Terminal) != 1 || r.Terminal[0] != LabelLegacyPlaceholder || !r.LastSeen.Equal(gapBase) {
		t.Errorf("the stub = %+v; want it adopted", r)
	}
	if r, _ := m.Get(kLook); len(r.Terminal) != 1 || r.Terminal[0] != MissingBody || !r.LastSeen.Equal(old) {
		t.Errorf("the look-alike = %+v; want it untouched", r)
	}

	// A large archive: the lookup does not read it.
	big, _, btok := gapManifest(t)
	for i := 0; i < 100000; i++ {
		big.Add(LiveKey(btok, fmt.Sprintf("mid:m%d@x", i)), Record{Path: fmt.Sprintf("%s/Inbox/m%d.html", btok, i), Folder: "Inbox", Present: true, LastSeen: old})
	}
	began := time.Now()
	for i := 0; i < 500; i++ {
		big.AdoptLegacyGraphStubs(btok, "", received.Add(time.Duration(i)*time.Second), gapBase, legacyIdentityFor)
	}
	if took := time.Since(began); took > 500*time.Millisecond {
		t.Errorf("500 adoptions over 100,000 records took %v: each one scanned the archive instead of reading the identity index", took)
	}
}
