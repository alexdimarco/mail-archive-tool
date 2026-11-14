package app

import (
	"context"
	"errors"
	"fmt"
	"log"
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

// The 1.7.2 patch review's P1: 1.7.1 never looked at the archive folder while a backup walked
// mail it already had; the first 1.7.2 build proved it every 10 seconds of that walk and failed the
// backup when a proof could not read the folder for about a second (a network folder reconnecting).
// Now nothing is proved on a timer while a run only reads, and a proof that cannot read keeps
// asking for about 30 seconds before the run ends as the archive location being unavailable.

// ma1444Outage makes target unreadable (mode 0) now and readable again after d; the returned func
// waits for that and puts the mode back.
func ma1444Outage(t *testing.T, target string, d time.Duration) func() {
	t.Helper()
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	mode := fi.Mode().Perm()
	t.Cleanup(func() { _ = os.Chmod(target, mode) })
	if err := os.Chmod(target, 0); err != nil {
		t.Fatal(err)
	}
	back := make(chan struct{})
	go func() { time.Sleep(d); _ = os.Chmod(target, mode); close(back) }()
	return func() { <-back }
}

// ma1444Unix skips where a file's mode cannot make it unreadable to its owner.
func ma1444Unix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a file's mode cannot make it unreadable to its owner on Windows: MA-1329's asks are its proof there")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode")
	}
}

// covers: MA-1444, R5
// P1: a healthy backup across a 5-second outage of its archive folder ends ok and records ok, as
// 1.7.1's did: an incremental run with nothing new whose lock file, or whose folder (its parent
// made unreadable), cannot be read for 5 s from its first message, with a proof due before every
// write (the interval made 0, so a timer would have proved at every message); a full run, which
// writes, whose lock file cannot be read for 5 s (its proof before the next write waits the outage
// out); and a Microsoft 365 capture with nothing new whose lock file cannot be read for 5 s from
// the start of its mailbox. Positive twin first: the same runs with no outage end ok.
func TestABackupAcrossAMomentsOutageEndsOk(t *testing.T) {
	ma1444Unix(t)
	old := archiveRecheckEvery
	archiveRecheckEvery = 0
	t.Cleanup(func() { archiveRecheckEvery = old })
	const outage = 5 * time.Second
	run := func(t *testing.T, mode export.Mode, what string) (string, time.Duration, error) {
		outA, outB := filepath.Join(tmpDir(t), "parent", "a"), tmpDir(t)
		storeA := ma1336TwoArchives(t, outA, outB)
		opts := Options{Inputs: []string{storeA}, Out: outA, Index: true, Pages: true, Mode: mode, Unattended: true}
		if what == "" {
			_, err := Run(context.Background(), opts, discard(), nil)
			return outA, 0, err
		}
		target := filepath.Join(outA, lockfile.Name)
		if what == "folder" {
			target = filepath.Dir(outA)
		}
		var wait func()
		began := time.Now()
		_, err := Run(context.Background(), opts, discard(), func(export.Stats) {
			if wait == nil {
				wait = ma1444Outage(t, target, outage)
			}
		})
		took := time.Since(began)
		if wait == nil {
			t.Fatal("premise: the walk was not reached")
		}
		wait()
		return outA, took, err
	}
	for _, mode := range []export.Mode{export.Incremental, export.Full} {
		out, _, err := run(t, mode, "")
		ma1329Healthy(t, fmt.Sprintf("positive twin (mode %v)", mode), out, err)
	}
	for _, c := range []struct {
		name string
		mode export.Mode
		what string
	}{{"nothing-new-lock", export.Incremental, "lock"}, {"nothing-new-folder", export.Incremental, "folder"}, {"full-lock", export.Full, "lock"}} {
		t.Run(c.name, func(t *testing.T) {
			out, took, err := run(t, c.mode, c.what)
			ma1329Healthy(t, c.name+" across a 5 s outage", out, err)
			t.Logf("%s: ended ok after %v", c.name, took.Round(time.Millisecond))
		})
	}

	t.Run("capture-nothing-new-lock", func(t *testing.T) {
		_, srv := newFakeGraphServer()
		defer srv.Close()
		capture := ma1337Capture(t, srv.URL)
		outG := filepath.Join(tmpDir(t), "archive")
		full := Options{Mode: export.Full, Index: true, Pages: true}
		if err := capture(outG, full, discard()); err != nil {
			t.Fatal(err)
		}
		ma1329Healthy(t, "positive twin: a capture with nothing new", outG, capture(outG, Options{Index: true, Pages: true}, discard()))
		var wait func()
		logger := log.New(&ma1337Logger{marker: "Reading mailbox", at: func() { wait = ma1444Outage(t, filepath.Join(outG, lockfile.Name), outage) }}, "", 0)
		err := capture(outG, Options{Index: true, Pages: true}, logger)
		if wait == nil {
			t.Fatal("premise: the mailbox was not reached")
		}
		wait()
		ma1329Healthy(t, "a capture with nothing new across a 5 s outage", outG, err)
	})
}

// covers: MA-1444, R5
// P1's first half on its own: a walk that only reads proves nothing at all, whatever the time
// since its last proof. Here a proof that cannot read gives up at once (the wait made 1 ms) and is
// due at every message (the interval made 0), and the lock file is unreadable from the first
// message of an incremental run with nothing new until its last: a proof on a timer during the
// walk would have ended the run, but none is asked, and the proof before the save, after the
// last message, reads the lock again. Positive twin first: the same walk with no outage ends ok,
// and its progress is reported at each message.
func TestAWalkThatOnlyReadsProvesNothing(t *testing.T) {
	ma1444Unix(t)
	ma1329Wait(t, time.Millisecond)
	old := archiveRecheckEvery
	archiveRecheckEvery = 0
	t.Cleanup(func() { archiveRecheckEvery = old })
	outA, outB := tmpDir(t), tmpDir(t)
	storeA := ma1336TwoArchives(t, outA, outB)
	opts := Options{Inputs: []string{storeA}, Out: outA, Index: true, Pages: true, Mode: export.Incremental, Unattended: true}
	calls := 0
	_, err := Run(context.Background(), opts, discard(), func(export.Stats) { calls++ })
	ma1329Healthy(t, "positive twin", outA, err)
	if calls < 3 {
		t.Fatalf("premise: the walk reported progress %d times, want one per message (at least 3)", calls)
	}
	lockPath := filepath.Join(outA, lockfile.Name)
	t.Cleanup(func() { _ = os.Chmod(lockPath, 0o600) })
	n := 0
	_, err = Run(context.Background(), opts, discard(), func(export.Stats) {
		n++
		switch n {
		case 1:
			if cerr := os.Chmod(lockPath, 0); cerr != nil {
				t.Fatal(cerr)
			}
		case calls:
			_ = os.Chmod(lockPath, 0o600)
		}
	})
	ma1329Healthy(t, "a walk that only reads, its lock unreadable from its first message to its last", outA, err)
	if n != calls {
		t.Fatalf("premise: the walk reported progress %d times, its twin %d", n, calls)
	}
}

// covers: MA-1444, R5
// A Microsoft 365 capture proves before its writes as a local walk does (MA-1330's bound, now kept
// by the proof before a message's first write, not by a timer): its folder comes to hold another
// archive as it starts reading its mailbox, and with a proof due before every write (the interval
// made 0) it writes no message page into the other archive at all and stops in plain words.
// Positive twin first: with no proof due before its writes (an hour), the same capture writes its
// messages' pages into the other archive until the proof before its save stops it.
func TestACaptureProvesBeforeItsWrites(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open")
	}
	_, srv := newFakeGraphServer()
	defer srv.Close()
	capture := ma1337Capture(t, srv.URL)
	pagesAdded := func(every time.Duration) (int, error) {
		old := archiveRecheckEvery
		archiveRecheckEvery = every
		defer func() { archiveRecheckEvery = old }()
		outG := filepath.Join(tmpDir(t), "archive")
		src := filepath.Join(tmpDir(t), "Inbox")
		maildirMessages(t, src, 1)
		outB := tmpDir(t)
		if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: outB}, discard(), nil); err != nil {
			t.Fatal(err)
		}
		before := ma1336Files(t, outB)
		swapped := false
		logger := log.New(&ma1337Logger{marker: "Reading mailbox", at: func() { swapped = true; ma1328Swap(t, outG, outG+"-drive1", outB) }}, "", 0)
		err := capture(outG, Options{Mode: export.Full, Index: true, Pages: true}, logger)
		if !swapped {
			t.Fatal("premise: the mailbox was not reached")
		}
		n := 0
		for _, d := range ma1336Changed(before, ma1336Files(t, outG)) {
			if strings.HasPrefix(d, "added ") && strings.HasSuffix(d, ".html") {
				n++
			}
		}
		return n, err
	}
	if n, err := pagesAdded(time.Hour); n < 2 || err == nil {
		t.Fatalf("positive twin: with no proof due before its writes the capture added %d pages to the other archive and ended %v", n, err)
	}
	n, err := pagesAdded(0)
	rc, msg := ma1328LockLost(err)
	assure.Refused(t, rc, msg, assure.Names("Backup stopped: the archive folder changed during the backup"),
		assure.NoSideEffect(func() bool {
			if n != 0 {
				t.Logf("the capture added %d message pages to the other archive", n)
			}
			return n == 0
		}))
}

// covers: MA-1444, R5
// P1's other side, at the real 30 s: a lock file that stays unreadable is not taken for a lost
// lock and does not fail the run at once; the proof before the next write keeps asking for about
// 30 seconds and the run then ends as the archive location being unavailable ("reconnect the
// drive or share"), kind archive_unwritable, having written nothing more and saved no manifest;
// the folder still holds this run's archive, so its failure is recorded there. Positive twin:
// TestABackupAcrossAMomentsOutageEndsOk's runs with no outage end ok.
func TestALockThatStaysUnreadableEndsUnavailableAfterTheWait(t *testing.T) {
	ma1444Unix(t)
	old := archiveRecheckEvery
	archiveRecheckEvery = 0
	t.Cleanup(func() { archiveRecheckEvery = old })
	outA, outB := tmpDir(t), tmpDir(t)
	storeA := ma1336TwoArchives(t, outA, outB)
	lockPath := filepath.Join(outA, lockfile.Name)
	t.Cleanup(func() { _ = os.Chmod(lockPath, 0o600) })
	m0 := ma1328Bytes(t, outA, ".mailarchive-manifest.json")
	done := false
	began := time.Now()
	_, err := Run(context.Background(), Options{Inputs: []string{storeA}, Out: outA, Index: true, Pages: true, Mode: export.Full, Unattended: true}, discard(), func(export.Stats) {
		if !done {
			done = true
			if cerr := os.Chmod(lockPath, 0); cerr != nil {
				t.Fatal(cerr)
			}
		}
	})
	took := time.Since(began)
	if !done {
		t.Fatal("premise: the walk was not reached")
	}
	_ = os.Chmod(lockPath, 0o600)
	var fatal *RunFatalError
	rc := 0
	if errors.As(err, &fatal) && FailureKind(err) == state.FailArchiveUnwritable && took >= 29*time.Second {
		rc = 1
	}
	t.Logf("ended after %v: %v", took.Round(time.Millisecond), err)
	assure.Refused(t, rc, fmt.Sprint(err), assure.Code(1), assure.Names(lockPath, "permission denied", "is unavailable", "reconnect the drive or share and re-run"),
		assure.Forbid("during the run", "panic", "goroutine"),
		assure.NoSideEffect(func() bool {
			lr, st, _ := state.ReadLastRun(outA)
			return ma1328Bytes(t, outA, ".mailarchive-manifest.json") == m0 && st == state.LastRunPresent && lr.Status == state.RunFailed
		}))
}

// covers: MA-1444, R5
// P8: an archive folder reached through a link whose drive is pulled mid-walk (the link's target
// gone) is the archive location being unavailable, not a lost lock: the proof follows the link
// (os.Stat). Both an incremental run with nothing new (its proof before the save) and a full run
// (its next write). The link's target is not made again. Positive twin first: through the link,
// with the drive in place, the run ends ok.
func TestALinkedArchiveFolderWhoseDriveIsPulledIsUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("making a symbolic link needs a privilege on Windows")
	}
	ma1329Wait(t, 300*time.Millisecond)
	for _, mode := range []export.Mode{export.Incremental, export.Full} {
		t.Run(map[export.Mode]string{export.Incremental: "nothing-new", export.Full: "full"}[mode], func(t *testing.T) {
			src := filepath.Join(tmpDir(t), "Inbox")
			maildirMessages(t, src, 4)
			drive := filepath.Join(tmpDir(t), "drive")
			real := filepath.Join(drive, "MailArchive")
			if err := os.MkdirAll(real, 0o755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(tmpDir(t), "MailArchive")
			if err := os.Symlink(real, link); err != nil {
				t.Fatal(err)
			}
			if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: link}, discard(), nil); err != nil {
				t.Fatal(err)
			}
			if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: link, Mode: mode, Unattended: true}, discard(), nil); err != nil {
				t.Fatalf("positive twin: %v", err)
			}
			pulled := false
			_, err := Run(context.Background(), Options{Inputs: []string{src}, Out: link, Mode: mode, Unattended: true}, discard(), func(export.Stats) {
				if !pulled {
					pulled = true
					if rerr := os.Rename(drive, drive+"-pulled"); rerr != nil {
						t.Fatal(rerr)
					}
				}
			})
			if !pulled {
				t.Fatal("premise: the walk was not reached")
			}
			rc := 0
			if FailureKind(err) == state.FailArchiveUnwritable {
				rc = 1
			}
			assure.Refused(t, rc, fmt.Sprint(err), assure.Code(1), assure.Names(link, "is unavailable", "reconnect the drive or share and re-run"),
				assure.Forbid("during the run", "panic", "goroutine"),
				assure.NoSideEffect(func() bool {
					_, serr := os.Lstat(drive)
					return errors.Is(serr, os.ErrNotExist)
				}))
		})
	}
}
