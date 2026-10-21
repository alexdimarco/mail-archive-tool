package state

import (
	"testing"
	"time"
)

// covers: MA-701
// The run-history store: an empty archive reads nil; appends keep only the most recent
// RunHistoryMax entries (newest last); and an entry built from a last-run record carries
// its status, error, counts and duration.
func TestMA701_RunHistoryStore(t *testing.T) {
	out := t.TempDir()

	if h, err := ReadRunHistory(out); err != nil || h != nil {
		t.Fatalf("an archive with no history must read nil, got %v (%v)", h, err)
	}

	for i := 0; i < RunHistoryMax+10; i++ {
		if err := AppendRunHistory(out, RunHistoryEntry{When: time.Unix(int64(i), 0).UTC(), Status: RunOK, Exported: i}); err != nil {
			t.Fatal(err)
		}
	}
	h, err := ReadRunHistory(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != RunHistoryMax {
		t.Fatalf("history should cap at %d entries, got %d", RunHistoryMax, len(h))
	}
	// The first 10 appends (Exported 0..9) were dropped; the window is newest-last.
	if h[0].Exported != 10 || h[len(h)-1].Exported != RunHistoryMax+9 {
		t.Fatalf("trim kept the wrong window: first=%d last=%d", h[0].Exported, h[len(h)-1].Exported)
	}

	lr := LastRun{Status: RunFailed, Error: "boom", Started: time.Unix(100, 0), Finished: time.Unix(103, 0), Exported: 5, Filled: 2, Mode: "full"}
	e := RunHistoryEntryFrom(lr)
	if e.Status != RunFailed || e.Error != "boom" || e.Exported != 5 || e.Filled != 2 || e.Mode != "full" || e.DurationMs != 3000 {
		t.Fatalf("RunHistoryEntryFrom mapped wrong: %+v", e)
	}
}
