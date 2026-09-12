package state

import (
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-218, R21, S37
// FoldEvents INVALIDATES a run whose header timestamp does not parse: its
// per-message events must not inherit the previous run's date. A history line
// that is valid JSON but carries an impossible date (hand-corruption, a garbled
// clock write, or a hostile writer) must not be able to resurrect a message that
// was gone as of D — nor hide one that was present (#3, adversarial 2026-09-12).
func TestFoldEventsSkipsUnparseableRunHeader(t *testing.T) {
	up, err := time.Parse(time.RFC3339, "2025-06-01T23:59:59Z")
	if err != nil {
		t.Fatal(err)
	}
	events := []HistoryEvent{
		{Run: 1, At: "2025-01-01T00:00:00Z"}, {K: "K", Folder: "Inbox"},
		{Run: 2, At: "2025-02-01T00:00:00Z"}, {K: "K", Gone: true},
		// run 3's header date does not parse. Its re-assert must be SKIPPED, not
		// dated to run 2 (which precedes upTo and would flip K back to present).
		{Run: 3, At: "2025-13-99T99:99:99Z"}, {K: "K", Folder: "Inbox"},
	}
	fold := FoldEvents(events, up)
	fs, ok := fold["K"]
	if !ok {
		t.Fatalf("K missing from the fold")
	}
	if fs.Present {
		t.Errorf("K reads Present at 2025-06-01, but it went gone at run 2 and run 3's header date is unparseable — a corrupt header falsified the point-in-time view (#3)")
	}
}

// covers: MA-221, R5, S37
// SetPresent flips ONLY a record's Present flag — the exact undo of a SweepGone
// gone-flip that the live path applies when the paired {k,gone} history append
// fails, so the saved manifest (the trailing anchor) never records a gone the
// log lacks (the manifest must never lead the log, #13, adversarial 2026-09-12).
// It leaves Folder/LastSeen untouched and returns false for a missing key.
func TestSetPresentRevertsOnlyPresent(t *testing.T) {
	old := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	thisRun := old.Add(24 * time.Hour)
	k := LiveKey("mbox", "mid:<g@x>")
	path := filepath.Join(t.TempDir(), "v4.json")
	writeFullManifest(t, path, 4, map[string]Record{
		k: {Path: "mbox/Inbox/g.html", Folder: "Inbox", ExportedAt: old, FirstFolder: "Inbox", FirstSeen: old, LastSeen: old, Present: true, Fingerprint: "gggggggggggggggg"},
	})
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	walked := NewWalkedFolders()
	walked.Mark("Inbox")
	if gone := m.SweepGone("mbox", walked, thisRun); len(gone) != 1 {
		t.Fatalf("SweepGone returned %v, want exactly one gone key", gone)
	}
	if r, _ := m.Get(k); r.Present {
		t.Fatalf("precondition: SweepGone did not flip Present=false")
	}

	if !m.SetPresent(k, true) {
		t.Fatalf("SetPresent returned false for an existing key")
	}
	r, _ := m.Get(k)
	if !r.Present {
		t.Errorf("SetPresent did not restore Present=true")
	}
	if r.Folder != "Inbox" || !r.LastSeen.Equal(old) {
		t.Errorf("SetPresent altered Folder/LastSeen: folder=%q lastSeen=%v, want Inbox / %v (the revert must leave everything but Present)", r.Folder, r.LastSeen, old)
	}
	if m.SetPresent("no-such-key", true) {
		t.Errorf("SetPresent on a missing key returned true, want false")
	}
}
