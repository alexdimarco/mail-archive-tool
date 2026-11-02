package archindex

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// addRow is a change adding p to the list.
func addRow(p string) func(Index) (Index, bool, error) {
	return func(idx Index) (Index, bool, error) {
		idx.Entries = append(idx.Entries, Entry{Path: p})
		return idx, true, nil
	}
}

func listedIn(t *testing.T, path, p string) bool {
	t.Helper()
	idx, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range idx.Entries {
		if e.Path == p {
			return true
		}
	}
	return false
}

// covers: MA-1014, R36
// Every change of Your archives (known-archives.json) is made under a lock that other MailArchive
// programs take too (the delete residuals' security pass, CON-07): a change made while another
// program holds the list's lock waits for it, then reads the list that program wrote, so neither
// change is lost; one that waits longer than the bound is refused, "Your archives (FILE) is being
// changed by another MailArchive window; try again", nothing written and its change never run.
// Positive twin first: with the lock free, a change is written.
func TestAChangeOfYourArchivesIsNeverLostToAnotherProgramsChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), Name)
	if err := UpdateFile(path, addRow("/archives/first")); err != nil || !listedIn(t, path, "/archives/first") {
		t.Fatalf("positive twin: a change with the lock free: %v", err)
	}

	// Another program holds the lock: it read the list, and writes its own change later.
	other, err := lockfile.Acquire(LockPath(path))
	if err != nil {
		t.Fatal(err)
	}
	stale, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- UpdateFile(path, addRow("/archives/mine")) }()
	time.Sleep(300 * time.Millisecond) // a change that did not wait would be written by now
	stale.Entries = append(stale.Entries, Entry{Path: "/archives/theirs"})
	if err := WriteFile(path, stale); err != nil {
		t.Fatal(err)
	}
	other.Release()
	if err := <-done; err != nil {
		t.Fatalf("the change that waited: %v", err)
	}
	for _, p := range []string{"/archives/first", "/archives/theirs", "/archives/mine"} {
		if !listedIn(t, path, p) {
			t.Errorf("%s was lost from Your archives", p)
		}
	}

	// Held past the bound: refused, nothing written, the change never run.
	orig := lockWait
	lockWait = 200 * time.Millisecond
	t.Cleanup(func() { lockWait = orig })
	held, err := lockfile.Acquire(LockPath(path))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	before, _ := os.ReadFile(path)
	ran := false
	err = UpdateFile(path, func(idx Index) (Index, bool, error) { ran = true; return addRow("/archives/late")(idx) })
	var busy *BusyError
	rc, msg := 0, ""
	if err != nil {
		rc, msg = 1, err.Error()
	}
	assure.Refused(t, rc, msg, assure.Code(1),
		assure.Names("Your archives ("+path+") is being changed by another MailArchive window; try again"),
		assure.NoSideEffect(func() bool {
			after, _ := os.ReadFile(path)
			return errors.As(err, &busy) && errors.Is(err, lockfile.ErrHeld) && !ran && bytes.Equal(before, after)
		}))
}
