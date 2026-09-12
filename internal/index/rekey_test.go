package index

import (
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-225, R8, S39
// Rekey applies the v3→v5 identity-collapse to the search index in one durable
// transaction (EC5): it RENAMES each survivor row old→new and DELETES each
// collapsed-away loser row, so the index tracks the manifest's re-keyed records.
// It is idempotent — a remap source that no longer exists (already re-keyed) is
// skipped — so a crash-and-retry re-run is safe.
func TestRekeyRenamesAndDrops(t *testing.T) {
	ix, err := Open(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	when := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	for _, k := range []string{"old1", "old2", "loser"} {
		if err := ix.Add("s", []string{"Inbox"}, mkMsg("s", "a", "me", "body", when), "Inbox/x.html", k); err != nil {
			t.Fatal(err)
		}
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}
	count := func(k string) int {
		var n int
		if err := ix.db.QueryRow(`SELECT count(*) FROM docs WHERE key=?`, k).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if err := ix.Rekey(map[string]string{"old1": "new1", "old2": "new2"}, []string{"loser"}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"new1", "new2"} {
		if count(k) != 1 {
			t.Errorf("survivor row not re-keyed to %q (found %d)", k, count(k))
		}
	}
	for _, k := range []string{"old1", "old2", "loser"} {
		if count(k) != 0 {
			t.Errorf("stale row %q still present after Rekey (found %d)", k, count(k))
		}
	}

	// Idempotent: re-running the same remap (sources now gone) is a clean no-op.
	if err := ix.Rekey(map[string]string{"old1": "new1", "old2": "new2"}, []string{"loser"}); err != nil {
		t.Fatalf("second Rekey (idempotent replay) errored: %v", err)
	}
	if count("new1") != 1 || count("new2") != 1 {
		t.Errorf("idempotent replay disturbed the re-keyed rows")
	}
}
