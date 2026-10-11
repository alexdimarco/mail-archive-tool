package state

import (
	"strings"
	"testing"
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
