package index

import (
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-118, R8, S29
// Range reports the oldest and newest dated messages in the index (date>0),
// in UTC; an index with no dated messages reports ok=false rather than a zero
// range. `status` prints it as the archive's coverage.
func TestRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "search.db")
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()

	// Empty index: no dated messages.
	if _, _, ok := ix.Range(); ok {
		t.Error("Range on an empty index reported ok=true")
	}

	oldest := time.Date(2019, 2, 1, 9, 0, 0, 0, time.UTC)
	mid := time.Date(2022, 6, 9, 9, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	// Add out of order to prove Range computes MIN/MAX, not first/last.
	for i, d := range []time.Time{mid, newest, oldest} {
		key := "Inbox\x00id" + string(rune('a'+i))
		if err := ix.Add("store", []string{"Inbox"}, mkMsg("s", "x", "me", "body", d), "Inbox/x.html", key); err != nil {
			t.Fatal(err)
		}
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}

	lo, hi, ok := ix.Range()
	if !ok {
		t.Fatal("Range reported ok=false on a populated index")
	}
	if !lo.Equal(oldest) {
		t.Errorf("oldest = %s, want %s", lo, oldest)
	}
	if !hi.Equal(newest) {
		t.Errorf("newest = %s, want %s", hi, newest)
	}
	if lo.Location() != time.UTC || hi.Location() != time.UTC {
		t.Errorf("Range must return UTC times: %s / %s", lo.Location(), hi.Location())
	}
}
