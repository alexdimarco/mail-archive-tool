package app

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

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

// covers: MA-319, MA-316, R1
// S2A-AI2 (K7's ledger side): a walk that could not read a folder must not erase the
// older gaps recorded in it. Run 1 leaves one gap (Drafts' first message is
// unreadable); run 2 reads the file with a table header that lies (the Drafts row
// size, the root's row size, the root's sub-folder column: one byte each), so the
// folder, or the whole store, reads as empty. Nothing was seen, so the old gap is
// kept as it was and the run says what it could not read; before the reader checked
// its tables, run 2 was clean and "no longer in the source" closed the gap.
func TestAWalkThatCouldNotReadAFolderKeepsItsOlderGaps(t *testing.T) {
	data := pstFixtureData(t)
	nids := pstMessageNIDs(t, data)
	broken := corruptNode(t, data, nids[0])
	for _, c := range []struct {
		name string
		off  int
	}{{"Drafts row size", 266653}, {"root row size", 18972}, {"root sub-folder column", 19044}} {
		t.Run(c.name, func(t *testing.T) {
			path := writeData(t, tmpDir(t), "old.pst", broken)
			out := tmpDir(t)
			if _, err := runLocal(t, out, path); !errors.Is(err, ErrPartial) {
				t.Fatalf("run 1: %v; want ErrPartial", err)
			}
			first := ledgerOf(t, out)
			if len(first) != 1 || first[0].Reason != "pst-message-unreadable" {
				t.Fatalf("run 1 ledger %+v; want the one unreadable-message gap", first)
			}
			ageLedger(t, out, 48*time.Hour)

			lying := append([]byte(nil), broken...)
			lying[c.off] ^= 0xFF
			writeData(t, filepath.Dir(path), "old.pst", lying)
			res, err := runLocal(t, out, path)
			if !errors.Is(err, ErrPartial) || !res.Partial {
				t.Errorf("run 2: err %v, partial %v; want ErrPartial: a folder could not be read", err, res.Partial)
			}
			if res.Gaps.ClosedGone != 0 {
				t.Errorf("run 2 closed %d gaps as 'no longer in the source' though it could not read the folder", res.Gaps.ClosedGone)
			}
			kept := false
			for _, g := range ledgerOf(t, out) {
				if g.Locator == first[0].Locator {
					kept = true
				}
			}
			if !kept {
				t.Errorf("run 2 ledger %+v lost the gap %q", ledgerOf(t, out), first[0].Locator)
			}
		})
	}
}
