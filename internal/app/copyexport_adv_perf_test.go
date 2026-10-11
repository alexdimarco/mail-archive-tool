package app

import "testing"

// covers: MA-446, R29
// EW-AC8 tombstone. Export activity polls History every 5 s, and History read every
// retained run record in full on every call (the header, every plan line and every
// progress line, twice for a record in a chain), so a few large retained exports meant
// seconds of CPU and gigabytes of garbage per poll beside a running export's own writes.
// A record that has not changed since the last call is now read from a cache keyed on
// its size and modification time: repeated calls scan nothing, and a record that
// grew (a new run, a seal) is scanned again, only it.
func TestCopyExportHistoryDoesNotRescanUnchangedRecords(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	var runs []string
	for range 3 {
		runs = append(runs, mustRun(t, e, cePlan(t, e, src, ceMsgs(4)), src).RunID)
	}
	first, err := e.History(nil, "")
	if err != nil || len(first) != 3 {
		t.Fatalf("History: %d cards (%v)", len(first), err)
	}
	before := summaryScans.Load()
	for range 5 {
		again, err := e.History(nil, "")
		if err != nil || len(again) != 3 || again[0].Label != first[0].Label || again[0].Exported != first[0].Exported {
			t.Fatalf("a cached History differs: %+v (%v)", again, err)
		}
	}
	if n := summaryScans.Load() - before; n != 0 {
		t.Errorf("five polls of unchanged records made %d full scans, want 0", n)
	}

	// A record that changed is read again, and only that one.
	fourth := mustRun(t, e, cePlan(t, e, src, ceMsgs(2)), src)
	before = summaryScans.Load()
	hist, err := e.History(nil, "")
	if err != nil || len(hist) != 4 || hist[0].Run != fourth.RunID || hist[0].Exported != 2 {
		t.Fatalf("History after a new run: %d cards, first %+v (%v)", len(hist), hist[0], err)
	}
	if n := summaryScans.Load() - before; n < 1 || n > 1 {
		t.Errorf("a new run made %d scans in the next poll, want exactly its own 1", n)
	}
}

// covers: MA-446, MA-437, R29
// EW-AC8 tombstone, the chain half (the fresh-eyes check found the EW-AC7 retry offers
// brought the rescans back). For a card in a chain of two or more parts, the retry
// offers are pruned by what the other parts exported, which read every part's record in
// full, twice, on every poll, for as long as a retry stayed pending. The answer is now
// kept per card against the size and modification time of every record of its chain:
// polls of a chain nobody has touched read nothing, and a part that was added to the
// chain makes the offers recomputed (part 1's retry goes away when a later part
// exports what it still offered).
func TestCopyExportHistoryDoesNotRescanAChainWithARetryPending(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	failing := map[string]bool{"M00002": true, "M00003": true}
	failIDs(src, failing)
	p1 := cePlan(t, e, src, ceMsgs(5))
	r1 := mustRun(t, e, p1, src)
	delete(failing, "M00002")
	in, err := e.RetryInput(r1.RunID, "failed", "alice@contoso.org")
	if err != nil || len(in.Messages) != 2 {
		t.Fatalf("the first retry: %v (%v)", in, err)
	}
	in.Dest = p1.Dest
	p2, err := e.PlanCopyExport(*in)
	if err != nil {
		t.Fatal(err)
	}
	r2 := mustRun(t, e, p2, src) // M00002 exports, M00003 keeps failing
	if r2.Counts.Exported != 1 {
		t.Fatalf("part 2: %s %+v", r2.Label, r2.Counts)
	}
	offers := func(run string) []string {
		hist, err := e.History(nil, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range hist {
			if c.Run == run {
				return c.Actions
			}
		}
		t.Fatalf("no card for %s", run)
		return nil
	}
	if !contains(offers(r1.RunID), ActExportFailed) || !contains(offers(r2.RunID), ActExportFailed) {
		t.Fatalf("both parts should still offer the retry of M00003: %v / %v", offers(r1.RunID), offers(r2.RunID))
	}

	scans, walks := summaryScans.Load(), messageWalks.Load()
	for range 5 {
		offers(r1.RunID)
	}
	if n, w := summaryScans.Load()-scans, messageWalks.Load()-walks; n != 0 || w != 0 {
		t.Errorf("five polls of an untouched two-part chain made %d record scans and %d message walks, want 0 and 0", n, w)
	}

	// A third part exports what is left: part 1 and part 2 stop offering it.
	delete(failing, "M00003")
	in3, err := e.RetryInput(r2.RunID, "failed", "alice@contoso.org")
	if err != nil || len(in3.Messages) != 1 {
		t.Fatalf("the second retry: %v (%v)", in3, err)
	}
	in3.Dest = p1.Dest
	p3, err := e.PlanCopyExport(*in3)
	if err != nil {
		t.Fatal(err)
	}
	if r3 := mustRun(t, e, p3, src); r3.Outcome != CopyOutcomeComplete {
		t.Fatalf("part 3: %s", r3.Label)
	}
	if contains(offers(r1.RunID), ActExportFailed) || contains(offers(r2.RunID), ActExportFailed) {
		t.Errorf("a cached card kept offering a retry a later part had done: %v / %v", offers(r1.RunID), offers(r2.RunID))
	}
}
