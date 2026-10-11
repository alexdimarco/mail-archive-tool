package app

import (
	"testing"

	"mail-archive-tool/internal/state"
)

// The FI-S2a adversarial pass's tombstones (docs/review-fault-isolation-s2a-adversarial.md):
// each test below was written red against the code the pass attacked.

// covers: MA-320, R10, R1
// S2A-AO1: a store whose display name carries a DEL byte (U+007F, which
// util.SanitizeSegment keeps) must not mint an unsafe store token: the token is the
// store's directory name, SafeToken refuses DEL, and the exporter's last-line guard
// (ErrUnsafeToken, run-fatal) then ended EVERY run at that input with a "tampered
// manifest" refusal. A store whose name only the file controls costs nothing: it is
// archived under a safe token, and the healthy input after it is still read.
func TestStoreNameWithDELDoesNotEndTheRun(t *testing.T) {
	parent := tmpDir(t)
	hostile := mboxStore(t, parent, "ho\x7fst\x7file", []string{"A"}, 3)
	healthy := mboxStore(t, parent, "healthy", []string{"B"}, 3)
	out := tmpDir(t)

	for run := 1; run <= 2; run++ {
		res, err := runLocal(t, out, hostile, healthy)
		if err != nil || res.Partial {
			t.Fatalf("run %d: err %v, partial %v; want nil, false: a DEL in a store's name is no reason to refuse the archive", run, err, res.Partial)
		}
	}
	m := loadArchive(t, out)
	if m.Len() != 6 {
		t.Errorf("%d records; want the 6 messages of both stores", m.Len())
	}
	for id, tok := range m.Stores {
		if !state.SafeToken(tok) {
			t.Errorf("store %s has the unsafe token %q", id, tok)
		}
	}
	if len(m.Stores) != 2 {
		t.Errorf("%d stores; want 2", len(m.Stores))
	}
}
