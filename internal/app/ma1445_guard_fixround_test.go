package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// The 1.7.2 patch's fix round: the adversarial review's P5 and P6/P7 and the friction review's F1,
// F4, F5 and F6, for local runs (the Microsoft 365 capture shares the guard and its words).

// ma1445Log is a run's log, kept whole and safe to write from more than one goroutine.
type ma1445Log struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *ma1445Log) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *ma1445Log) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// ma1445Plant writes a one-byte file at p, an hour old (a crashed run's leftover).
func ma1445Plant(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
}

// ma1445Token is the store folder an archive of one store holds.
func ma1445Token(t *testing.T, out string) string {
	t.Helper()
	ents, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			return e.Name()
		}
	}
	t.Fatalf("no store folder in %s", out)
	return ""
}

// covers: MA-1445, R5
// P5: the crash-leftover sweep at a run's start walks the whole folder by path and removes what a
// crashed run left; after a swap that path is the other archive's (in a rotation, its own store
// folder of the same name). Each removal is proved first: here the drive is swapped once the sweep
// has removed one leftover of A's own, and the other archive's stale temp and its zip with no page
// are both still there, nothing in it is added, removed or written again, and the run stops in
// plain words. Positive twin first: in its own archive the sweep removes a stale temp in the
// folder, one in a message folder and an orphan zip, and the run ends ok.
func TestTheCrashSweepProvesBeforeEachRemoval(t *testing.T) {
	_, store := imapProfile(t, "imap.a.example", nil, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("a", 2)})
	own := tmpDir(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{store}, Out: own, Index: true, Pages: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	tok := ma1445Token(t, own)
	left := []string{filepath.Join(own, ".mailarchive-stale1.tmp"), filepath.Join(own, tok, ".mailarchive-stale2.tmp"), filepath.Join(own, tok, "zz-only-a-zip-attachments.zip")}
	for _, p := range left {
		ma1445Plant(t, p)
	}
	if _, err := Run(context.Background(), Options{Inputs: []string{store}, Out: own, Index: true, Pages: true}, discard(), nil); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	for _, p := range left {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("positive twin: the sweep left %s", p)
		}
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open")
	}
	outA, outB := tmpDir(t), tmpDir(t)
	for _, o := range []string{outA, outB} {
		if _, err := Run(context.Background(), Options{Inputs: []string{store}, Out: o, Index: true, Pages: true}, discard(), nil); err != nil {
			t.Fatal(err)
		}
	}
	tok = ma1445Token(t, outB)
	ma1445Plant(t, filepath.Join(outA, ".mailarchive-stale1.tmp"))
	ma1445Plant(t, filepath.Join(outB, tok, ".mailarchive-stale2.tmp"))
	ma1445Plant(t, filepath.Join(outB, tok, "zz-only-a-zip-attachments.zip"))
	before := ma1336Files(t, outB)
	swapped := false
	logger := log.New(&ma1337Logger{marker: "swept orphan", at: func() { swapped = true; ma1328Swap(t, outA, outA+"-drive1", outB) }}, "", 0)
	_, err := Run(context.Background(), Options{Inputs: []string{store}, Out: outA, Index: true, Pages: true}, logger, nil)
	if !swapped {
		t.Fatal("premise: the sweep removed nothing")
	}
	rc, msg := ma1328LockLost(err)
	assure.Refused(t, rc, msg, assure.Names(filepath.Join(outA, lockfile.Name), "Backup stopped: the archive folder changed during the backup"),
		assure.NoSideEffect(func() bool { return ma1336Untouched(t, outA, before) }))
}

// covers: MA-1445, R5
// P5 for a Microsoft 365 capture, whose sweep removes stale temps only: in a rotation of one
// mailbox (two archives of it, the same store folder in each), the drive is swapped once the sweep
// has removed one leftover of its own, and the other archive's stale temp is still there, nothing
// in it added, removed or written again; the capture stops in plain words. Positive twin first: in
// its own archive the capture's sweep removes a stale temp in the folder and one in its store
// folder, and the capture ends ok.
func TestACapturesCrashSweepProvesBeforeEachRemoval(t *testing.T) {
	_, srv := newFakeGraphServer()
	defer srv.Close()
	capture := ma1337Capture(t, srv.URL)
	full := Options{Mode: export.Full, Index: true, Pages: true}
	own := filepath.Join(tmpDir(t), "archive")
	if err := capture(own, full, discard()); err != nil {
		t.Fatal(err)
	}
	tok := ma1445Token(t, own)
	left := []string{filepath.Join(own, ".mailarchive-stale1.tmp"), filepath.Join(own, tok, ".mailarchive-stale2.tmp")}
	for _, p := range left {
		ma1445Plant(t, p)
	}
	if err := capture(own, full, discard()); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	for _, p := range left {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("positive twin: the sweep left %s", p)
		}
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open")
	}
	outA, outB := filepath.Join(tmpDir(t), "a"), filepath.Join(tmpDir(t), "b")
	for _, o := range []string{outA, outB} {
		if err := capture(o, full, discard()); err != nil {
			t.Fatal(err)
		}
	}
	tok = ma1445Token(t, outB)
	ma1445Plant(t, filepath.Join(outA, ".mailarchive-stale1.tmp"))
	ma1445Plant(t, filepath.Join(outB, tok, ".mailarchive-stale2.tmp"))
	before := ma1336Files(t, outB)
	swapped := false
	logger := log.New(&ma1337Logger{marker: "swept orphan", at: func() { swapped = true; ma1328Swap(t, outA, outA+"-drive1", outB) }}, "", 0)
	err := capture(outA, full, logger)
	if !swapped {
		t.Fatal("premise: the sweep removed nothing")
	}
	rc, msg := ma1328LockLost(err)
	assure.Refused(t, rc, msg, assure.Names(filepath.Join(outA, lockfile.Name), "Backup stopped: the archive folder changed during the backup"),
		assure.NoSideEffect(func() bool { return ma1336Untouched(t, outA, before) }))
}

// covers: MA-1446, R5
// F5: once a run has begun on an archive, nothing it does makes the archive's own folder again: a
// folder moved away mid-run with nothing in its place, its parent writable (a folder renamed, a
// mount point the person owns), ends the run as the archive location being unavailable
// ("reconnect the drive or share"), with no folder made at the old path and the moved folder
// whole. 1.7.2's first build made the folder again and wrote the run's new message files there.
// Positive twin first: a brand-new archive's first run makes its folder.
func TestAFolderMovedAwayMidRunIsNeverMadeAgain(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 6)
	out := filepath.Join(tmpDir(t), "drive", "Archive")
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out}, discard(), nil); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, ".mailarchive-manifest.json")); err != nil {
		t.Fatalf("positive twin: the first run did not make its archive: %v", err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open")
	}
	moved := false
	began := time.Now()
	_, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Full}, discard(), func(export.Stats) {
		if !moved {
			moved = true
			if rerr := os.Rename(out, out+"-moved"); rerr != nil {
				t.Fatal(rerr)
			}
		}
	})
	if !moved {
		t.Fatal("premise: the walk was not reached")
	}
	// The walk's own write found the folder gone, so no proof after waits the 30 s for it again.
	if took := time.Since(began); took > 15*time.Second {
		t.Errorf("the run took %v to end on a folder its walk found gone", took)
	}
	rc := 0
	if FailureKind(err) == state.FailArchiveUnwritable {
		rc = 1
	}
	assure.Refused(t, rc, fmt.Sprint(err), assure.Code(1), assure.Names(out, "is unavailable", "reconnect the drive or share and re-run"),
		assure.NoSideEffect(func() bool {
			_, made := os.Lstat(out)
			_, whole := os.Stat(filepath.Join(out+"-moved", ".mailarchive-manifest.json"))
			return errors.Is(made, os.ErrNotExist) && whole == nil
		}))
}

// covers: MA-1447, R5
// F1, F4 and F6: a backup stopped because its folder is no longer its own says so in plain words,
// by what it found at its end, with the lock's own sentence after "Details:" and without the
// system's error: another drive or folder now at its path (here a folder swapped in after the
// walk; nothing recorded there, and the log's warning says the result was not written into it);
// its own lock file deleted (not on Windows, which will not delete an open file); its own lock
// file changed. Each word of each sentence is pinned here. Positive twin first: the same run with
// nothing changed ends ok.
func TestAStoppedBackupSaysWhatHappenedInPlainWords(t *testing.T) {
	outA, outB := tmpDir(t), tmpDir(t)
	storeA := ma1336TwoArchives(t, outA, outB)
	if _, err := Run(context.Background(), Options{Inputs: []string{storeA}, Out: outA, Index: true, Pages: true}, discard(), nil); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	lockIn := func(out string) string { return filepath.Join(out, lockfile.Name) }
	own := func(t *testing.T, act func(out string)) (string, error, state.LastRun) {
		t.Helper()
		out, other := tmpDir(t), tmpDir(t)
		store := ma1336TwoArchives(t, out, other)
		done := false
		_, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Index: true, Pages: true, Mode: export.Full}, discard(), func(export.Stats) {
			if !done {
				done = true
				act(out)
			}
		})
		if !done {
			t.Fatal("premise: the walk was not reached")
		}
		lr, _, _ := state.ReadLastRun(out)
		return out, err, lr
	}

	t.Run("changed", func(t *testing.T) {
		out, err, lr := own(t, func(out string) {
			if werr := os.WriteFile(lockIn(out), []byte("pid=1\nnonce=another-run\n"), 0o600); werr != nil {
				t.Fatal(werr)
			}
		})
		want := "Backup stopped: MailArchive's lock file in the archive folder (.mailarchive.lock) was changed during the backup: another backup of this archive may have started, or another app wrote to the file. Your archive is fine. Run the backup again once any other backup of it has finished. Details: lock " + lockIn(out) + " was replaced during the run."
		rc, msg := ma1328LockLost(err)
		assure.Refused(t, rc, msg, assure.Names(want),
			assure.NoSideEffect(func() bool { return lr.Status == state.RunFailed && lr.Error == want }))
	})
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not delete or rename an open file or folder: the changed case is its proof")
	}
	t.Run("deleted", func(t *testing.T) {
		out, err, lr := own(t, func(out string) {
			if rerr := os.Remove(lockIn(out)); rerr != nil {
				t.Fatal(rerr)
			}
		})
		want := "Backup stopped: MailArchive's lock file in the archive folder (.mailarchive.lock) was deleted during the backup, by a person or by a cleaning or sync app. Your archive is fine. Run the backup again. If this keeps happening, stop that app from cleaning the archive folder. Details: lock " + lockIn(out) + " was removed during the run."
		rc, msg := ma1328LockLost(err)
		assure.Refused(t, rc, msg, assure.Names(want), assure.Forbid("no such file", "panic", "goroutine"),
			assure.NoSideEffect(func() bool { return lr.Status == state.RunFailed && lr.Error == want }))
	})
	t.Run("another-folder", func(t *testing.T) {
		outA, outB := tmpDir(t), tmpDir(t)
		storeA := ma1336TwoArchives(t, outA, outB)
		before := ma1336Files(t, outB)
		swapped := false
		afterWalkForTest = func() {
			if !swapped {
				swapped = true
				ma1328Swap(t, outA, outA+"-drive1", outB)
			}
		}
		defer func() { afterWalkForTest = nil }()
		logs := &ma1445Log{}
		_, err := Run(context.Background(), Options{Inputs: []string{storeA}, Out: outA, Index: true, Pages: true}, log.New(logs, "", 0), nil)
		if !swapped {
			t.Fatal("premise: the walk's end was not reached")
		}
		want := "Backup stopped: the archive folder changed during the backup. Another drive or folder is now at " + outA + " (a drive was swapped or disconnected, or the folder was moved or renamed), or another backup took it over. Nothing more was saved there. Connect the drive that holds this archive, then run the backup again. Details: lock " + lockIn(outA) + " was replaced during the run."
		warning := "warning: the folder at " + outA + " changed during this backup (another drive or folder is there now, or another backup took it over), so this backup's result was not written into it.\n"
		rc, msg := ma1328LockLost(err)
		assure.Refused(t, rc, msg, assure.Names(want),
			assure.NoSideEffect(func() bool {
				if !strings.Contains(logs.String(), warning) {
					t.Logf("the log lacks the warning %q:\n%s", warning, logs.String())
					return false
				}
				return ma1336Untouched(t, outA, before)
			}))
	})
}

// covers: MA-1452, R5
// P6 and P7: the run's own "running" record binds it to its folder. A brand-new archive's first
// backup whose lock file is removed (written over on Windows) before its first save has no
// manifest to compare, and 1.7.2's first build recorded nothing there, so its record stayed
// "running"; now it is recorded failed in its own archive, with a run history entry and
// BACKUP-NEEDS-ATTENTION.txt, as any lost lock in its own archive. And two archives whose manifests
// hold the same bytes (two empty first runs) are no longer taken for one: swapped in, the other
// archive gets no record, run history entry or notice. Positive twins first: the brand-new
// archive's first backup left alone ends ok; the two manifests are the same bytes.
func TestTheRunningRecordBindsANewArchiveAndTellsSameBytesApart(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 3)
	fresh := filepath.Join(tmpDir(t), "fresh")
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: fresh}, discard(), nil); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	out := filepath.Join(tmpDir(t), "archive")
	lost := false
	_, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out}, discard(), func(export.Stats) {
		if lost {
			return
		}
		lost = true
		lockPath := filepath.Join(out, lockfile.Name)
		if runtime.GOOS == "windows" { // Windows will not remove an open file: written over stands in
			if werr := os.WriteFile(lockPath, []byte("pid=1\nnonce=another-run\n"), 0o600); werr != nil {
				t.Fatal(werr)
			}
			return
		}
		if rerr := os.Remove(lockPath); rerr != nil {
			t.Fatal(rerr)
		}
	})
	if !lost {
		t.Fatal("premise: the walk was not reached")
	}
	rc, msg := ma1328LockLost(err)
	assure.Refused(t, rc, msg, assure.Names("Backup stopped: MailArchive's lock file in the archive folder", "Your archive is fine"),
		assure.NoSideEffect(func() bool {
			lr, st, _ := state.ReadLastRun(out)
			h, _ := state.ReadRunHistory(out)
			_, notice := os.Stat(filepath.Join(out, state.AttentionName))
			_, manifest := os.Stat(filepath.Join(out, ".mailarchive-manifest.json"))
			if st != state.LastRunPresent || lr.Status != state.RunFailed || len(h) != 1 || notice != nil || manifest == nil {
				t.Logf("record %q (%v), history %d, notice %v, manifest saved %v", lr.Status, st, len(h), notice == nil, manifest == nil)
				return false
			}
			return true
		}))

	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open")
	}
	empty := filepath.Join(tmpDir(t), "Empty")
	maildirMessages(t, empty, 0)
	outA, outB := filepath.Join(tmpDir(t), "a"), filepath.Join(tmpDir(t), "b")
	for _, o := range []string{outA, outB} {
		if _, err := Run(context.Background(), Options{Inputs: []string{empty}, Out: o}, discard(), nil); err != nil && !errors.Is(err, ErrPartial) {
			t.Logf("empty first run into %s: %v", filepath.Base(o), err)
		}
	}
	if a, b := ma1328Bytes(t, outA, ".mailarchive-manifest.json"), ma1328Bytes(t, outB, ".mailarchive-manifest.json"); a == "" || a != b {
		t.Fatalf("premise: the two manifests are not the same bytes (%d and %d bytes)", len(a), len(b))
	}
	lrB, _, _ := state.ReadLastRun(outB)
	histB, _ := state.ReadRunHistory(outB)
	before := ma1328Top(t, outB)
	swapped := false
	_, err = Run(context.Background(), Options{Inputs: []string{empty, src}, Out: outA}, discard(), func(export.Stats) {
		if !swapped {
			swapped = true
			ma1328Swap(t, outA, outA+"-drive1", outB)
		}
	})
	if !swapped {
		t.Fatal("premise: the walk was not reached")
	}
	rc, msg = ma1328LockLost(err)
	assure.Refused(t, rc, msg, assure.Names("Backup stopped: the archive folder changed during the backup"),
		assure.NoSideEffect(func() bool {
			lr, _, _ := state.ReadLastRun(outA)
			h, _ := state.ReadRunHistory(outA)
			_, notice := os.Stat(filepath.Join(outA, state.AttentionName))
			if d := ma1336Changed(before, ma1328Top(t, outA)); len(d) > 0 || lr.Status != lrB.Status || len(h) != len(histB) || notice == nil {
				t.Logf("written into the other archive: %v; record %q (was %q), history %d (was %d), notice %v", d, lr.Status, lrB.Status, len(h), len(histB), notice == nil)
				return false
			}
			return true
		}))
}
