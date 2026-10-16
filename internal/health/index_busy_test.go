package health

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/index"
)

// covers: MA-398, R18, S29
// A search index that could not be read only because another connection kept
// it locked past the busy timeout (Gather sets IndexBusy from index.Busy, here
// on a really held file) is judged in use, never damaged: with the archive
// lock free it is a WARN index_busy that says to try again — never RED
// index_unreadable and never the rebuild remedy — and while a run holds the
// lock it is a note that leaves the posture alone. The Messages line says
// "busy". Positive twin: the same open failure without the busy classification
// (a damaged file) is still the RED index_unreadable.
func TestBusyIndexIsJudgedInUseNotDamaged(t *testing.T) {
	// Gathered for real: another connection keeps search.db exclusively locked
	// past the read-only open's busy timeout (fault injection; the index, the
	// open and Gather are the real ones).
	// Best-effort cleanup: SQLite can hold search.db briefly past Close on
	// Windows, which must not fail the test.
	out, err := os.MkdirTemp("", "healthbusy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(out) })
	dbPath := filepath.Join(out, "search.db")
	ix, err := index.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	holder, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	holder.SetMaxOpenConns(1)
	if _, err := holder.Exec(`PRAGMA locking_mode=EXCLUSIVE; BEGIN; UPDATE meta SET version = version; COMMIT;`); err != nil {
		holder.Close()
		t.Fatal(err)
	}
	gathered := Gather(out, "")
	holder.Close()
	if !gathered.HasIndexFile || gathered.HasIndex || !gathered.IndexBusy {
		t.Fatalf("a held search.db was not gathered as busy: hasFile=%v opened=%v busy=%v err=%v", gathered.HasIndexFile, gathered.HasIndex, gathered.IndexBusy, gathered.IndexErr)
	}
	if rep := Assess(gathered, time.Now()); !hasCode(rep, "index_busy") || hasCode(rep, "index_unreadable") {
		t.Fatalf("a held search.db judged %v, want index_busy and no index_unreadable", rep.Reasons)
	}

	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	in.HasIndex, in.Indexed, in.HasIndexFile = false, 0, true
	in.IndexErr = errors.New("search index /a/search.db is busy (database is locked (5) (SQLITE_BUSY))")

	if rep := Assess(in, now); rep.Posture != "RED" || !hasCode(rep, "index_unreadable") || hasCode(rep, "index_busy") {
		t.Fatalf("positive twin: an unclassified open failure must be RED index_unreadable: %s %v", rep.Posture, rep.Reasons)
	}

	in.IndexBusy = true
	rep := Assess(in, now)
	warn := ""
	for i, c := range rep.Codes {
		if c == "index_busy" {
			warn = rep.Reasons[i]
		}
	}
	assure.Reached(t, warn, "the index_busy reason with the archive lock free")
	if rep.Posture != "WARN" || !strings.HasPrefix(warn, "WARN:") || hasCode(rep, "index_unreadable") || strings.Contains(warn, "reindex -rebuild") || !strings.Contains(warn, "run status again") {
		t.Fatalf("a busy index with the lock free: posture %s, reasons %v — want a WARN index_busy naming the retry, no rebuild", rep.Posture, rep.Reasons)
	}
	if !summaryHas(in, rep, "search index busy") {
		t.Errorf("the Messages line does not say the index is busy: %v", Summary(in, rep))
	}

	in.LockHeld = true
	rep = Assess(in, now)
	if !hasCode(rep, "index_busy") || rep.Posture != "GREEN" || hasCode(rep, "index_unreadable") {
		t.Fatalf("a busy index under a run's lock must be a note only: posture %s, reasons %v", rep.Posture, rep.Reasons)
	}
}
