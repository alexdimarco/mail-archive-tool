package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/state"
)

// The Phase-2b-1 adversarial pass's findings on the runs (2026-10-25).

// covers: MA-755, R37
// The sealed record's failures are named by what they are: a record a newer
// MailArchive wrote is record_newer ("upgrade"), never "damaged" (FID-1); a lock
// found lost at the commit point is lock_lost, never a failed save (CON-3) — and it
// ends the run, as a checkpoint's own check does.
func TestTheSealedRecordsFailuresAreNamedByWhatTheyAre(t *testing.T) {
	const secret = "/Users/someone/SECRET"
	for _, c := range []struct {
		err  error
		want string
	}{
		{recordRefusal(secret, fmt.Errorf("%w: %s", container.ErrNewerFormat, secret)), state.FailRecordNewer},
		{manifestSaveFailure(secret, fmt.Errorf("%w: %s", container.ErrLockLost, secret)), state.FailLockLost},
	} {
		if got := FailureKind(c.err); got != c.want {
			t.Fatalf("FailureKind(%v) = %q, want %q", c.err, got, c.want)
		}
	}
	var lost error
	stopOnLostLock(errors.New("disk full"), &lost)
	if lost != nil {
		t.Fatal("a save that failed for another reason must not end the run as a lost lock")
	}
	stopOnLostLock(fmt.Errorf("%w: replaced", container.ErrLockLost), &lost)
	if !errors.Is(lost, container.ErrLockLost) {
		t.Fatal("a save that found the lock lost must end the run")
	}
}

// covers: MA-755, R37
// FC-4: a refused run's record is minimised whenever the encryption check cannot
// tell — the minimised record is safe for either kind of archive.
func TestAnUnreadableArchiveCountsAsEncryptedForItsRecords(t *testing.T) {
	file := filepath.Join(tmpDir(t), "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !encryptedAt(filepath.Join(file, "out")) {
		t.Fatal("an archive the check cannot read must count as encrypted for its records")
	}
	if encryptedAt(tmpDir(t)) {
		t.Fatal("positive twin: an empty folder is not encrypted")
	}
}

// covers: MA-755, R37
// CON-2: when a new archive's first record lands but its file list cannot be saved,
// the run goes on — the record is not lost, and the run reports inventory_failed,
// never "the backup's progress could not be saved".
func TestAFirstFileListFailureDoesNotAbortTheRun(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	// The file list's top file cannot be written: a folder sits at its name.
	if err := os.Mkdir(filepath.Join(out, container.InventoryName), 0o700); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key}, discard(), nil)
	if k := FailureKind(err); k != state.FailInventory {
		t.Fatalf("the run's kind is %q (%v), want %q", k, err, state.FailInventory)
	}
	if res.Stats.Exported == 0 {
		t.Fatal("the run must go on and archive the mail")
	}
	if m := loadSealed(t, out, key); m.Len() == 0 {
		t.Fatal("the sealed record must hold the archived mail")
	}
}
