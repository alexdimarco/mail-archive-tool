package state

import (
	"path/filepath"
	"testing"
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
