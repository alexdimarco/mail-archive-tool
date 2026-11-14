package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// The 1.7.2 guard proves far more often than 1.7.1 checked its lock (every 10 seconds, at every
// source and before every write of the tail, not after 1,000 messages), and a proof that fails
// fails the backup and fires its alert. These tests hold the other side of MA-1329: a healthy
// archive is never stopped by its own proofs.

// ma1329Healthy says a run ended ok and recorded so: its error nil, its record "ok", no
// BACKUP-NEEDS-ATTENTION.txt, its manifest and root page there.
func ma1329Healthy(t *testing.T, what, out string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: a healthy run ended %v", what, err)
	}
	lr, st, _ := state.ReadLastRun(out)
	if st != state.LastRunPresent || lr.Status != state.RunOK {
		t.Fatalf("%s: the record says %q (%v), want ok", what, lr.Status, st)
	}
	assure.Reached(t, ma1328Bytes(t, out, ".mailarchive-manifest.json"), what+": the manifest")
	assure.Reached(t, ma1328Bytes(t, out, "index.html"), what+": the root page")
	if _, serr := os.Stat(filepath.Join(out, state.AttentionName)); serr == nil {
		t.Fatalf("%s: a healthy run left %s", what, state.AttentionName)
	}
}

// covers: MA-1329, R5
// A healthy archive is never stopped by its proofs, here due at every message (the interval made
// 0): a full run, an incremental run with nothing new, a run while another program holds the lock
// file open for reading the whole walk (a scanner, `status`), a run given the archive's path in
// another letter case where the file system ignores case (macOS, Windows), a run of two sources
// (a proof before each save), and a Microsoft 365 capture. Each ends ok and records ok.
func TestAHealthyRunIsNeverStoppedByItsProofs(t *testing.T) {
	old := archiveRecheckEvery
	archiveRecheckEvery = 0
	t.Cleanup(func() { archiveRecheckEvery = old })
	outA, outB := tmpDir(t), tmpDir(t)
	storeA := ma1336TwoArchives(t, outA, outB)
	run := func(out string, mode export.Mode, inputs []string, progress ProgressFunc) error {
		_, err := Run(context.Background(), Options{Inputs: inputs, Out: out, Index: true, Pages: true, Mode: mode, Unattended: true}, discard(), progress)
		return err
	}
	ma1329Healthy(t, "a full run", outA, run(outA, export.Full, []string{storeA}, nil))
	ma1329Healthy(t, "an incremental run with nothing new", outA, run(outA, export.Incremental, []string{storeA}, nil))

	var held *os.File
	err := run(outA, export.Full, []string{storeA}, func(export.Stats) {
		if held == nil {
			f, oerr := os.Open(filepath.Join(outA, lockfile.Name))
			if oerr != nil {
				t.Fatalf("open the lock file as another reader: %v", oerr)
			}
			held = f
		}
	})
	if held == nil {
		t.Fatal("premise: the walk was not reached")
	}
	held.Close()
	ma1329Healthy(t, "a run with the lock file held open by another reader", outA, err)

	other := filepath.Join(filepath.Dir(outA), strings.ToUpper(filepath.Base(outA)))
	if other != outA {
		if _, serr := os.Stat(filepath.Join(other, ".mailarchive-manifest.json")); serr == nil {
			ma1329Healthy(t, "a run given the path in another letter case", outA, run(other, export.Full, []string{storeA}, nil))
		} else {
			t.Logf("this file system tells letter case apart (%s): the case form is not run here", runtime.GOOS)
		}
	}

	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 3)
	ma1329Healthy(t, "a run of two sources", outA, run(outA, export.Incremental, []string{storeA, src}, nil))

	_, srv := newFakeGraphServer()
	defer srv.Close()
	capture := ma1337Capture(t, srv.URL)
	outG := filepath.Join(tmpDir(t), "archive")
	full := Options{Mode: export.Full, Index: true, Pages: true}
	if err := capture(outG, full, discard()); err != nil {
		t.Fatal(err)
	}
	ma1329Healthy(t, "a Microsoft 365 capture", outG, capture(outG, full, discard()))
}

// covers: MA-1329, R5
// A proof that fails is asked again before it counts (archiveProofRetries), so a moment in which a
// healthy archive's folder or lock file cannot be read (a network folder reconnecting, a scanner)
// is not a lost lock; one that keeps failing is. Positive twins first: a lock that stays unreadable
// stops the run as a lost lock ("permission denied"), and a proof whose answer stays no is no after
// every retry. Then: a proof that fails once and is then yes is yes; and a real run whose lock file
// is unreadable for a moment (made unreadable at one message, readable again 20 ms later, with the
// proof due at every message) ends ok.
func TestAMomentsUnreadableLockIsAskedAgain(t *testing.T) {
	dir := tmpDir(t)
	denied := errors.New("lock was removed during the run (permission denied)")
	asks := 0
	always := &archiveGuard{out: dir, stillHeld: func() error { asks++; return denied }}
	if err := always.prove(); !errors.Is(err, denied) || asks != len(archiveProofRetries)+1 {
		t.Fatalf("twin: a proof whose answer stays no gave %v after %d asks, want it after %d", err, asks, len(archiveProofRetries)+1)
	}
	asks = 0
	if err := always.lockGone(); !errors.Is(err, denied) || asks != len(archiveProofRetries)+1 {
		t.Fatalf("twin: the record's question gave %v after %d asks", err, asks)
	}
	gone := &archiveGuard{out: filepath.Join(dir, "pulled"), stillHeld: func() error { return nil }}
	var fatal *RunFatalError
	if err := gone.prove(); !errors.As(err, &fatal) || !strings.Contains(err.Error(), "is unavailable") {
		t.Fatalf("twin: a folder that is not there gave %v, want the run-fatal \"unavailable\"", err)
	}
	asks = 0
	once := &archiveGuard{out: dir, stillHeld: func() error {
		asks++
		if asks == 1 {
			return denied
		}
		return nil
	}}
	if err := once.prove(); err != nil || asks != 2 {
		t.Fatalf("a proof that failed once gave %v after %d asks, want yes after 2", err, asks)
	}

	if runtime.GOOS == "windows" {
		t.Skip("a file's mode cannot make it unreadable to its owner on Windows: the asks above are its proof")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode: the asks above are its proof")
	}
	old := archiveRecheckEvery
	archiveRecheckEvery = 0
	t.Cleanup(func() { archiveRecheckEvery = old })
	unreadable := func(t *testing.T, back time.Duration) (string, error) {
		outA, outB := tmpDir(t), tmpDir(t)
		storeA := ma1336TwoArchives(t, outA, outB)
		lockPath := filepath.Join(outA, lockfile.Name)
		t.Cleanup(func() { _ = os.Chmod(lockPath, 0o600) })
		done := false
		_, err := Run(context.Background(), Options{Inputs: []string{storeA}, Out: outA, Index: true, Pages: true, Mode: export.Full}, discard(), func(export.Stats) {
			if done {
				return
			}
			done = true
			if cerr := os.Chmod(lockPath, 0); cerr != nil {
				t.Fatal(cerr)
			}
			if back > 0 {
				go func() { time.Sleep(back); _ = os.Chmod(lockPath, 0o600) }()
			}
		})
		if !done {
			t.Fatal("premise: the walk was not reached")
		}
		return outA, err
	}
	_, err := unreadable(t, 0)
	rc, msg := ma1328LockLost(err)
	assure.Refused(t, rc, msg, assure.Names(lockfile.Name, "permission denied", "run again"))
	out, err := unreadable(t, 20*time.Millisecond)
	ma1329Healthy(t, "a run whose lock file was unreadable for a moment", out, err)
}
