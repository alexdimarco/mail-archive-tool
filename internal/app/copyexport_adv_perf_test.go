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
