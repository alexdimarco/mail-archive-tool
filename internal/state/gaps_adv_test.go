package state

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// The FI-S2a adversarial pass's ledger and token tombstones
// (docs/review-fault-isolation-s2a-adversarial.md).

// covers: MA-320, R10
// S2A-AO1: the token minted for any store name is a SafeToken. The name comes from
// the data file (a PST's PidTagDisplayName), and util.SanitizeSegment keeps DEL, which
// SafeToken refuses: the exporter's last-line guard then ended every run at the file.
// Two names that differ only in such a byte never merge, and a gap recorded under the
// token is kept (ObserveGap silently refuses an unsafe token).
func TestTokenIsSafeForAnyStoreName(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"support", "supp\x7fr\x7f", "\x7f", "a\x7fb", "ab", "\u0085c1\x7f", "con\x7f"}
	seen := map[string]string{}
	for i, name := range names {
		tok := m.Token("path:/x/"+string(rune('a'+i)), name)
		if !SafeToken(tok) {
			t.Errorf("store name %q gave the unsafe token %q", name, tok)
		}
		if prev, dup := seen[tok]; dup {
			t.Errorf("store names %q and %q share the token %q", prev, name, tok)
		}
		seen[tok] = name
		g := CaptureGap{Token: tok, Folder: "F", Scope: GapScopeItem, Locator: "ident:x", Reason: "export-failed", Class: ClassFillable}
		if _, created := m.ObserveGap(g, gapBase); !created {
			t.Errorf("a gap under the token %q of store %q was not recorded", tok, name)
		}
	}
	if plain := m.Token("path:/x/plain", "support"); plain != "support" && !SafeToken(plain) {
		t.Errorf("the plain name's token %q changed", plain)
	}
}

// covers: MA-323, MA-316, R1, R10
// S2A-AO3: the lifetime caps (P13) bound what a hostile source costs the ledger: the
// overflow entries that absorb the excess are per folder, so a source whose faults
// each name a DIFFERENT folder (a hierarchy table whose rows loop back to the root
// under 100,000 distinct names) made one overflow entry per fault, with no cap, and
// every new key rescanned the whole ledger. At most maxGapFolders folder entries and
// maxGapOverflow overflow entries are kept per token; the rest are counted, exactly,
// in the token's own overflow entry. The cost of the whole stays linear enough to
// finish in a second, where 16,000 faults took two seconds and 100,000 about 77.
func TestOverflowEntriesAreBoundedPerToken(t *testing.T) {
	m, path, tok := gapManifest(t)
	const faults = 6000
	start := time.Now()
	for run := 0; run < 2; run++ {
		runAt := gapBase.Add(time.Duration(run) * 48 * time.Hour)
		for i := 0; i < faults; i++ {
			f := fmt.Sprintf("Loop/f%05d", i)
			m.ObserveGap(CaptureGap{Token: tok, Folder: f, Scope: GapScopeFolder, Locator: "folder:" + f + "#cycle", Reason: "folder-cycle", Class: ClassTerminal}, runAt)
		}
		if n := m.GapCount(); n > maxGapFolders+maxGapOverflow+1 {
			t.Fatalf("run %d: %d ledger entries after %d faults in distinct folders; want at most %d folder entries and %d overflow entries and the token's own", run+1, n, faults, maxGapFolders, maxGapOverflow)
		}
		if got := m.MissingGapCount(); got != faults {
			t.Errorf("run %d: the ledger accounts for %d missing folders; want exactly %d (nothing dropped, nothing double-counted)", run+1, got, faults)
		}
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("12,000 observations took %v; the ledger rescans itself per fault", d)
	}
	if m2 := reload(t, m, path); m2.GapCount() != m.GapCount() || m2.MissingGapCount() != faults {
		t.Errorf("after Save and Load: %d entries counting %d; want %d counting %d", m2.GapCount(), m2.MissingGapCount(), m.GapCount(), faults)
	}
}
