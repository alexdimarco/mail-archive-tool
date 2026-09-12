package state

import (
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-208, R1, R5, S38, S37
// SweepGone is gone-detection by full reconciliation (§3.4); this proves its
// SCOPE guards over a v4 manifest holding several records of one mailbox token
// (plus one of a DIFFERENT token). Sweeping with the walked set {Inbox} and a
// thisRun after the prior run:
//   - marks gone ONLY a still-Present record whose folder was walked AND whose
//     LastSeen precedes this run (it was not re-observed, so it left the live
//     mailbox), flipping ONLY Present and keeping the record's last-known Folder
//     and LastSeen so a past view still shows where/when it last lived (the file
//     stays on disk — R13);
//   - NEVER marks a record in an unwalked/EXCLUDED folder (an excluded folder can
//     never trigger gone), one observed THIS run (LastSeen==thisRun), one already
//     gone (Present=false), or a record of ANOTHER mailbox token (out of scope);
//   - returns the swept keys sorted so the caller appends one {k,gone} event per.
func TestSweepGoneScopedToWalkedFolders(t *testing.T) {
	old := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC) // last seen in a PRIOR run
	thisRun := old.Add(24 * time.Hour)

	kGone := LiveKey("mbox", "mid:<gone@x>")    // Inbox, Present, LastSeen<run  → gone
	kSeen := LiveKey("mbox", "mid:<seen@x>")    // Inbox, Present, LastSeen==run → retained
	kExcl := LiveKey("mbox", "mid:<excl@x>")    // Deleted Items (unwalked)      → retained
	kDead := LiveKey("mbox", "mid:<dead@x>")    // Inbox, already gone           → skipped
	kOther := LiveKey("other", "mid:<other@x>") // a DIFFERENT token             → out of scope

	path := filepath.Join(t.TempDir(), "v4.json")
	writeFullManifest(t, path, 4, map[string]Record{
		kGone:  {Path: "mbox/Inbox/g.html", Folder: "Inbox", ExportedAt: old, FirstFolder: "Inbox", FirstSeen: old, LastSeen: old, Present: true, Fingerprint: "gggggggggggggggg"},
		kSeen:  {Path: "mbox/Inbox/s.html", Folder: "Inbox", ExportedAt: old, FirstFolder: "Inbox", FirstSeen: old, LastSeen: thisRun, Present: true, Fingerprint: "ssssssssssssssss"},
		kExcl:  {Path: "mbox/Deleted Items/e.html", Folder: "Deleted Items", ExportedAt: old, FirstFolder: "Deleted Items", FirstSeen: old, LastSeen: old, Present: true, Fingerprint: "eeeeeeeeeeeeeeee"},
		kDead:  {Path: "mbox/Inbox/d.html", Folder: "Inbox", ExportedAt: old, FirstFolder: "Inbox", FirstSeen: old, LastSeen: old, Present: false, Fingerprint: "dddddddddddddddd"},
		kOther: {Path: "other/Inbox/o.html", Folder: "Inbox", ExportedAt: old, FirstFolder: "Inbox", FirstSeen: old, LastSeen: old, Present: true, Fingerprint: "oooooooooooooooo"},
	})
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	walked := NewWalkedFolders()
	walked.Mark("Inbox") // "Deleted Items" was NOT walked (excluded by default, T7)

	gone := m.SweepGone("mbox", walked, thisRun)

	// Exactly the one deleted-from-a-walked-folder record.
	if len(gone) != 1 || gone[0] != kGone {
		t.Fatalf("SweepGone returned %v, want exactly [%q]", gone, kGone)
	}
	if r, _ := m.Get(kGone); r.Present {
		t.Errorf("the gone record is still Present after the sweep")
	}
	// The flip keeps the last-known Folder and LastSeen (a KEPT file; a past view
	// still shows where/when it last lived — only Present changed, R13).
	if r, _ := m.Get(kGone); r.Folder != "Inbox" || !r.LastSeen.Equal(old) {
		t.Errorf("the sweep altered the gone record's Folder/LastSeen: %q/%v, want Inbox/%v", r.Folder, r.LastSeen, old)
	}

	// Everything else untouched.
	if r, _ := m.Get(kSeen); !r.Present {
		t.Errorf("a record observed THIS run (LastSeen==thisRun) was wrongly marked gone")
	}
	if r, _ := m.Get(kExcl); !r.Present {
		t.Errorf("a record in an EXCLUDED/unwalked folder was marked gone (scope violation, §3.4)")
	}
	if r, _ := m.Get(kDead); r.Present {
		t.Errorf("an already-gone record was resurrected by the sweep")
	}
	if r, _ := m.Get(kOther); !r.Present {
		t.Errorf("a record of ANOTHER mailbox token was swept (token scope violation)")
	}
}

// covers: MA-207, R5, R1, S38, S37
// A sweep over a run that walked NO folders — the empty observed-set a partial or
// aborted walk carries — marks nothing gone even though a still-Present record
// last seen before this run exists: with no folder in scope, there is no evidence
// the message left the mailbox. This is the state-level twin of the graph
// partial-run guard: gone is concluded only from folders actually walked.
func TestSweepGoneEmptyWalkMarksNothing(t *testing.T) {
	old := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	thisRun := old.Add(24 * time.Hour)
	k := LiveKey("mbox", "mid:<a@x>")

	path := filepath.Join(t.TempDir(), "v4.json")
	writeFullManifest(t, path, 4, map[string]Record{
		k: {Path: "mbox/Inbox/a.html", Folder: "Inbox", ExportedAt: old, FirstFolder: "Inbox", FirstSeen: old, LastSeen: old, Present: true, Fingerprint: "aaaaaaaaaaaaaaaa"},
	})
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	gone := m.SweepGone("mbox", NewWalkedFolders(), thisRun)
	if len(gone) != 0 {
		t.Fatalf("a sweep over an empty walked-set returned %v, want none (a partial walk marks nothing gone)", gone)
	}
	if r, _ := m.Get(k); !r.Present {
		t.Errorf("a record was marked gone though no folder was walked (§3.4)")
	}
}
