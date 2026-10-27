package app

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/vault"
)

// covers: MA-798, MA-797, R37
// The sweep of readers' scratch indexes goes by the OS lock a live reader holds
// beside its index, not by the pid its name carries: a held one is kept; one whose
// lock is free, or that has none, is removed with its lock file — its pid alive
// (reused) or not; a lock file left alone is removed too (§7.4; the P3-2a pass,
// LEAK-03, FID-10).
func TestTheReaderScratchSweepGoesByItsLock(t *testing.T) {
	root := t.TempDir()
	orig := scratchRoot
	scratchRoot = func() (string, error) { return root, nil }
	defer func() { scratchRoot = orig }()
	out, id := t.TempDir(), vault.ID{7}

	live, err := ReaderScratch(out, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	hold, err := HoldReaderScratch(live)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	os.WriteFile(live, []byte("index"), 0o600)
	dir := filepath.Dir(live)
	reused := filepath.Join(dir, "reader-1-1.db") // pid 1 is alive: a reused pid
	os.WriteFile(reused, []byte("index"), 0o600)
	unheld := filepath.Join(dir, "reader-1-2.db")
	os.WriteFile(unheld, []byte("index"), 0o600)
	os.WriteFile(unheld+".lock", nil, 0o600)
	orphanLock := filepath.Join(dir, "reader-1-3.db.lock")
	os.WriteFile(orphanLock, nil, 0o600)

	SweepReaderScratch(out, id)
	for p, want := range map[string]bool{live: true, live + ".lock": true, reused: false, unheld: false, unheld + ".lock": false, orphanLock: false} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Errorf("%s: kept %v, want %v", filepath.Base(p), err == nil, want)
		}
	}
	hold.Close()
	SweepReaderScratch(out, id)
	_, err = os.Stat(live)
	assure.Reached(t, os.IsNotExist(err), "a released reader's index swept")
}
