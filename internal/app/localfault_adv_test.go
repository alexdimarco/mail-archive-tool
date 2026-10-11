package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

// advSourceRun is a source's walk over a fresh archive, with no data file behind it:
// the tests below feed it the faults a reader would deliver.
func advSourceRun(t *testing.T) (*sourceRun, *state.Manifest) {
	t.Helper()
	out := tmpDir(t)
	m := loadArchive(t, out)
	lr := &localRun{exp: &export.Exporter{OutDir: out, Manifest: m, Log: discard()}, out: out, runAt: time.Now().UTC(), logger: discard()}
	return &sourceRun{lr: lr, token: "tok", path: "old.pst", reportsFaults: true}, m
}

func advFolderFault(folder string) source.Fault {
	return source.Fault{Reason: "folder-unreadable", Scope: state.GapScopeFolder, Class: state.ClassTerminal,
		Folder: strings.Split(folder, "/"), Locator: "folder:" + folder + "#messages", Detail: "the message table could not be read"}
}

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

// covers: MA-323, MA-319, R1
// S2A-AI7 (and S2A-AO6): the closure guard compares the folders a run could not read
// with the ledger's entries in the ledger's spelling. The ledger strips control and
// bidi characters from a folder and cuts it at 512 bytes; the guard held the raw path,
// so for a folder with a right-to-left override or a C1 control in its name, or a path
// past 512 bytes, it never matched and the older gaps of the folder that could not be
// read were closed as "no longer in the source". Twin: a plain folder, and a gap in
// ANOTHER folder, which a fault in this one does not protect.
func TestFolderFaultGuardMatchesTheLedgersSpelling(t *testing.T) {
	long := strings.Repeat("Long/", 120) + "Tail" // 604 bytes
	for _, folder := range []string{"Plain", "Pro‮jects", "Inbox\u0085x", "In\u009bbox", long} {
		t.Run(fmt.Sprintf("%.12q", folder), func(t *testing.T) {
			s, m := advSourceRun(t)
			seed := time.Now().Add(-48 * time.Hour)
			m.ObserveGap(state.CaptureGap{Token: "tok", Folder: folder, Scope: state.GapScopeItem, Locator: "pst-nid:u/1", Reason: "pst-message-unreadable", Class: state.ClassTerminal}, seed)
			m.ObserveGap(state.CaptureGap{Token: "tok", Folder: "Elsewhere", Scope: state.GapScopeItem, Locator: "pst-nid:u/2", Reason: "pst-message-unreadable", Class: state.ClassTerminal}, seed)
			s.onReaderFault(advFolderFault(folder))
			closed := m.CloseUnobserved("tok", s.covered, s.lr.runAt, time.Time{})
			left := m.CaptureGapsSorted()
			// The fault's own folder entry and the old gap of the folder it names stay; the
			// gap of the folder nothing was wrong with is closed (it was fully read).
			if closed != 1 || len(left) != 2 {
				t.Errorf("closed %d, ledger %+v; want only the gap in Elsewhere closed and the gap in the unreadable folder kept", closed, left)
			}
		})
	}
}

// covers: MA-323, MA-316, R10
// S2A-AO3: the list of folders a run could not read (it guards the closure) holds each
// folder once and is bounded: a hostile source whose faults each name a different
// folder grew it by a string per fault, and the guard scanned it for every older gap.
// Past the bound the run closes NOTHING (it cannot say which folders it read), which
// fails closed.
func TestFolderFaultListIsBounded(t *testing.T) {
	t.Run("the same folder many times", func(t *testing.T) {
		s, _ := advSourceRun(t)
		for i := 0; i < 5000; i++ {
			s.onReaderFault(advFolderFault("Drafts"))
		}
		if len(s.folderFaults) != 1 {
			t.Errorf("%d entries for one folder; want 1", len(s.folderFaults))
		}
	})
	t.Run("a different folder every time", func(t *testing.T) {
		s, m := advSourceRun(t)
		seed := time.Now().Add(-48 * time.Hour)
		m.ObserveGap(state.CaptureGap{Token: "tok", Folder: "Elsewhere", Scope: state.GapScopeItem, Locator: "pst-nid:u/2", Reason: "pst-message-unreadable", Class: state.ClassTerminal}, seed)
		for i := 0; i < 20000; i++ {
			s.onReaderFault(advFolderFault(fmt.Sprintf("Loop/f%05d", i)))
		}
		if len(s.folderFaults) > 5000 {
			t.Errorf("%d folders listed after 20,000 faults; want a bounded list", len(s.folderFaults))
		}
		if closed := m.CloseUnobserved("tok", s.covered, s.lr.runAt, time.Time{}); closed != 0 {
			t.Errorf("closed %d gaps after the list overflowed; want none: a run that cannot say which folders it read closes nothing", closed)
		}
	})
}
