package app

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// ma1338Outside is what changed in an encrypted archive's folder outside its sealed message files
// (units/ and chunks/): its record, run history, attention notice, sealed manifest, vault and the
// rest.
func ma1338Outside(changed []string) []string {
	var d []string
	for _, c := range changed {
		rel := c[strings.IndexByte(c, ' ')+1:]
		if !strings.HasPrefix(rel, export.UnitsDir+"/") && !strings.HasPrefix(rel, "chunks/") {
			d = append(d, c)
		}
	}
	return d
}

// covers: MA-1338, R5
// J2: an encrypted archive's run proves its folder too, as a plaintext run does, before every save
// and on time during its walk (its binding is its key, proved by its saves, and its lock; its record
// has no id, and no production code outside the unlock path may read the vault's, MA-726). Here
// encrypted archive A's folder comes to hold encrypted archive B while A's run walks its store: the
// run stops as a lost lock, in the lock's words, and writes nothing into B outside its sealed message
// files: no record, no run history entry, no BACKUP-NEEDS-ATTENTION.txt, no sealed manifest. An
// encrypted walk proves after every message it writes, not only on time: whether or not the proof on
// time is due, the walk reads at most one more message after the swap, so B receives at most the
// sealed files of the message in flight. Positive twin first: without the swap A's run ends ok.
func TestAnEncryptedRunWhoseFolderIsSwappedRecordsNothingInTheOther(t *testing.T) {
	src := encryptedSourcePST(t)
	outA, kA := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: outA, Key: kA}, discard(), nil); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open")
	}
	after := func(t *testing.T, every time.Duration) (int, error, []string) {
		outA, kA := encryptedOut(t)
		outB, kB := encryptedOut(t)
		if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: outB, Key: kB}, discard(), nil); err != nil {
			t.Fatalf("B's own backup: %v", err)
		}
		before := ma1336Files(t, outB)
		old := archiveRecheckEvery
		archiveRecheckEvery = every
		defer func() { archiveRecheckEvery = old }()
		swapped, n := false, 0
		_, err := Run(context.Background(), Options{Inputs: []string{src}, Out: outA, Key: kA, Mode: export.Full}, discard(), func(export.Stats) {
			if swapped {
				n++
				return
			}
			swapped = true
			ma1328Swap(t, outA, outA+"-drive1", outB)
		})
		if !swapped {
			t.Fatal("premise: the walk was not reached")
		}
		if lr, st, _ := state.ReadLastRun(outA); st != state.LastRunPresent || lr.Status != state.RunOK {
			t.Errorf("B's last-run record no longer says its own backup was ok: %+v (%v)", lr, st)
		}
		return n, err, ma1338Outside(ma1336Changed(before, ma1336Files(t, outA)))
	}
	for _, c := range []struct {
		name  string
		every time.Duration
	}{
		{"proof-on-time-due-at-every-message", 0},
		{"proof-on-time-not-due", time.Hour}, // only the proof after each message written stops it
	} {
		t.Run(c.name, func(t *testing.T) {
			n, err, outside := after(t, c.every)
			if n > 1 {
				t.Errorf("the walk read %d messages after its folder came to hold another archive", n)
			}
			rc, msg := ma1328LockLost(err)
			assure.Refused(t, rc, msg, assure.Names("Details: the lock file was", "during the run", "the archive folder changed during the backup", "then run the backup again"),
				assure.NoSideEffect(func() bool {
					if len(outside) > 0 {
						t.Logf("written into the other archive: %v", outside)
						return false
					}
					return true
				}))
		})
	}
}
