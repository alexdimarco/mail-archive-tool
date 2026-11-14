package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
	"mail-archive-tool/internal/tbcomplete"
)

// The 1.7.2 patch's final round: the adversarial re-check's Q2, Q3 and Q5, and the friction
// re-walk's G3.

// covers: MA-1454, R5
// Q2: a Cancel (the terminal's Ctrl-C, Run now's Cancel, MailArchive Desktop's Shut down, which
// cancels the running backup and gives it 15 s) pressed while a proof waits for the folder is
// honoured at once, not after the wait, and the backup is recorded cancelled, not failed. At the
// guard: a proof waiting on a lock file that stays unreadable, its run cancelled 300 ms in, returns
// the cancel within a second, with the real 30-second wait, and keeps nothing (a later proof asks
// again). Positive twin first: the same proof with no cancel would wait (here, made short, it ends
// "unavailable" after the wait).
func TestACancelDuringTheWaitIsHonouredAtOnce(t *testing.T) {
	dir := tmpDir(t)
	denied := &lockfile.LostError{Path: filepath.Join(dir, lockfile.Name), Err: os.ErrPermission}
	var fatal *RunFatalError
	func() {
		ma1329Wait(t, 300*time.Millisecond)
		twin := &archiveGuard{out: dir, stillHeld: func() error { return denied }, ctx: context.Background()}
		if err := twin.prove(); !errors.As(err, &fatal) {
			t.Fatalf("positive twin: an uncancelled wait ended %v, want the run-fatal \"unavailable\"", err)
		}
	}()
	archiveUnreadableFor = 30 * time.Second // the real wait, put back by ma1329Wait's cleanup too
	ctx, cancel := context.WithCancel(context.Background())
	g := &archiveGuard{out: dir, stillHeld: func() error { return denied }, ctx: ctx}
	time.AfterFunc(300*time.Millisecond, cancel)
	began := time.Now()
	err := g.prove()
	if !errors.Is(err, context.Canceled) || time.Since(began) > time.Second || g.lost != nil {
		t.Fatalf("a proof cancelled during its wait gave %v after %v, kept %v; want the cancel at once, kept nothing", err, time.Since(began), g.lost)
	}
}

// covers: MA-1454, R5
// Q2 in a real run, as an ordinary user on Unix: a full backup whose lock file becomes unreadable
// at its first message (another program changed its permissions) waits at its next proof; cancelled
// one second into that wait, it ends cancelled within a few seconds, not after the 30-second wait
// and not as "unavailable", and its record says cancelled (no BACKUP-NEEDS-ATTENTION.txt). The same
// with the whole folder unreachable (its parent unreadable): it ends cancelled as promptly, and its
// log never claims the folder now holds another archive. Positive twin first: the same backup
// cancelled at its first message with nothing unreadable is recorded cancelled too.
func TestABackupCancelledWhileItWaitsIsRecordedCancelled(t *testing.T) {
	ma1444Unix(t)
	old := archiveRecheckEvery
	archiveRecheckEvery = 0
	t.Cleanup(func() { archiveRecheckEvery = old })
	run := func(t *testing.T, what string) (string, time.Duration, string, error) {
		outA, outB := filepath.Join(tmpDir(t), "parent", "a"), tmpDir(t)
		storeA := ma1336TwoArchives(t, outA, outB)
		target, mode := filepath.Join(outA, lockfile.Name), os.FileMode(0o600)
		if what == "folder" {
			target, mode = filepath.Dir(outA), 0o755
		}
		t.Cleanup(func() { _ = os.Chmod(target, mode) })
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var cancelledAt time.Time
		done := false
		logs := &ma1445Log{}
		_, err := Run(ctx, Options{Inputs: []string{storeA}, Out: outA, Index: true, Pages: true, Mode: export.Full}, log.New(logs, "", 0), func(export.Stats) {
			if done {
				return
			}
			done = true
			if what != "" {
				if cerr := os.Chmod(target, 0); cerr != nil {
					t.Fatal(cerr)
				}
				time.AfterFunc(time.Second, func() { cancelledAt = time.Now(); cancel() })
				return
			}
			cancelledAt = time.Now()
			cancel()
		})
		if !done {
			t.Fatal("premise: the walk was not reached")
		}
		_ = os.Chmod(target, mode)
		return outA, time.Since(cancelledAt), logs.String(), err
	}
	recorded := func(out string) (string, bool) {
		lr, _, _ := state.ReadLastRun(out)
		_, notice := os.Stat(filepath.Join(out, state.AttentionName))
		return lr.Status, notice == nil
	}
	out, _, _, err := run(t, "")
	if status, notice := recorded(out); !errors.Is(err, context.Canceled) || status != state.RunCancelled || notice {
		t.Fatalf("positive twin: a cancelled backup ended %v, recorded %q, notice %v", err, status, notice)
	}
	out, after, _, err := run(t, "lock")
	status, notice := recorded(out)
	rc := 0
	if errors.Is(err, context.Canceled) && after < 5*time.Second {
		rc = 130
	}
	t.Logf("lock: ended %v after the cancel: %v; recorded %q", after.Round(time.Millisecond), err, status)
	assure.Refused(t, rc, fmt.Sprint(err), assure.Code(130), assure.Names("canceled"), assure.Forbid("unavailable", "panic", "goroutine"),
		assure.NoSideEffect(func() bool { return status == state.RunCancelled && !notice }))

	_, after, logged, err := run(t, "folder")
	rc = 0
	if errors.Is(err, context.Canceled) && after < 5*time.Second {
		rc = 130
	}
	t.Logf("folder: ended %v after the cancel: %v", after.Round(time.Millisecond), err)
	assure.Refused(t, rc, fmt.Sprint(err), assure.Code(130), assure.Names("canceled"), assure.Forbid("unavailable", "panic", "goroutine"),
		assure.NoSideEffect(func() bool {
			if strings.Contains(logged, "no longer this archive's") {
				t.Logf("the log of a cancelled backup claims another archive:\n%s", logged)
				return false
			}
			return true
		}))
}

// covers: MA-1455, R5
// Q3: a lock file "not found" counts as deleted only when a fresh listing of its folder (one that
// goes to the drive or share, not a cached look) succeeds and does not list it; a network client may
// say "not found" of what it cannot reach just now. A folder that cannot be listed (here: its read
// right taken away, as an ordinary user on Unix) is "could not read", waited on and, when it stays
// so, the archive location being unavailable, not "the lock file was deleted"; a lock file the
// listing still shows is "could not read" too, and the proof says yes once it can read the lock
// again. Positive twin first: a folder listed afresh without the lock file is a definite "deleted"
// at the first ask.
func TestALockNotFoundIsCheckedAgainstAFreshListing(t *testing.T) {
	ma1329Wait(t, 300*time.Millisecond)
	dir := tmpDir(t)
	lockPath := filepath.Join(dir, lockfile.Name)
	removed := &lockfile.LostError{Path: lockPath, Err: os.ErrNotExist}
	asks := 0
	gone := &archiveGuard{out: dir, stillHeld: func() error { asks++; return removed }}
	if err := gone.prove(); !errors.Is(err, removed) || asks != 1 {
		t.Fatalf("positive twin: a lock file missing from a fresh listing gave %v after %d asks, want \"deleted\" at once", err, asks)
	}

	listed := tmpDir(t)
	if err := os.WriteFile(filepath.Join(listed, lockfile.Name), []byte("nonce=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	asks = 0
	stillThere := &archiveGuard{out: listed, stillHeld: func() error {
		asks++
		if asks == 1 {
			return &lockfile.LostError{Path: filepath.Join(listed, lockfile.Name), Err: os.ErrNotExist}
		}
		return nil
	}}
	if err := stillThere.prove(); err != nil || asks != 2 {
		t.Fatalf("a lock file \"not found\" that its folder still lists gave %v after %d asks, want it asked again and yes", err, asks)
	}

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a folder's mode cannot hide its listing from its owner here: the asks above are the proof")
	}
	unlisted := tmpDir(t)
	if err := os.Chmod(unlisted, 0o311); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unlisted, 0o755) })
	asks = 0
	g := &archiveGuard{out: unlisted, stillHeld: func() error {
		asks++
		return &lockfile.LostError{Path: filepath.Join(unlisted, lockfile.Name), Err: os.ErrNotExist}
	}}
	err := g.prove()
	var fatal *RunFatalError
	rc := 0
	if errors.As(err, &fatal) && asks > 1 {
		rc = 1
	}
	assure.Refused(t, rc, fmt.Sprint(err), assure.Code(1), assure.Names(unlisted, "is unavailable"), assure.Forbid("deleted", "panic", "goroutine"))
}

// covers: MA-1456, R5
// Q5: a proof that waits for the folder says so in the run's log, once as it starts ("waiting up
// to 30 seconds for the archive folder to be reachable again…", with the system's words) and once
// as it ends: reachable again, still not reachable after the wait, or the backup cancelled.
// Positive twin first: a proof that reads the folder at once says nothing.
func TestTheWaitForTheFolderIsToldInTheLog(t *testing.T) {
	dir := tmpDir(t)
	denied := &lockfile.LostError{Path: filepath.Join(dir, lockfile.Name), Err: os.ErrPermission}
	logs := &ma1445Log{}
	logger := log.New(logs, "", 0)
	quiet := &archiveGuard{out: dir, stillHeld: func() error { return nil }, logger: logger}
	if err := quiet.prove(); err != nil || logs.String() != "" {
		t.Fatalf("positive twin: a proof that read at once gave %v and logged %q", err, logs.String())
	}
	asks := 0
	back := &archiveGuard{out: dir, logger: logger, stillHeld: func() error {
		asks++
		if asks < 3 {
			return denied
		}
		return nil
	}}
	if err := back.prove(); err != nil {
		t.Fatal(err)
	}
	waitLine := "waiting up to 30 seconds for the archive folder to be reachable again… (permission denied)\n"
	backLine := "the archive folder can be reached again after less than a second.\n"
	if got := logs.String(); got != waitLine+backLine {
		t.Fatalf("a wait that ended with the folder back logged %q, want %q", got, waitLine+backLine)
	}
	// Its length in words, as the start line says "30 seconds" (the final check's wording-9).
	for d, want := range map[time.Duration]string{400 * time.Millisecond: "less than a second", 1200 * time.Millisecond: "1 second", 4 * time.Second: "4 seconds"} {
		if got := durationWords(d); got != want {
			t.Errorf("durationWords(%v) is %q, want %q", d, got, want)
		}
	}

	logs = &ma1445Log{}
	ma1329Wait(t, 2*time.Second)
	stays := &archiveGuard{out: dir, logger: log.New(logs, "", 0), stillHeld: func() error { return denied }}
	_ = stays.prove()
	if got := logs.String(); !strings.Contains(got, "waiting up to 2 seconds for the archive folder to be reachable again…") || !strings.Contains(got, "the archive folder was still not reachable after 2 seconds.") {
		t.Fatalf("a wait that ran out logged %q", got)
	}

	logs = &ma1445Log{}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	stopped := &archiveGuard{out: dir, ctx: ctx, logger: log.New(logs, "", 0), stillHeld: func() error { return denied }}
	_ = stopped.prove()
	if got := logs.String(); !strings.Contains(got, "stopped waiting for the archive folder: the backup was cancelled.") {
		t.Fatalf("a wait cut short by a cancel logged %q", got)
	}
}

// covers: MA-1457, R5
// G3: the run-fatal sentence of an archive location that cannot be reached tells a person who swaps
// drives what to do (put this archive's drive back), keeps the kept-messages note, and moves the
// system's words to the end, after "Details:". Since 1.7.2 never makes the archive's folder again
// (MA-1446), a drive swapped with a gap between the two ends here, so the sentence must fit it. The
// whole sentence is pinned. Positive twin first: a full drive keeps its own sentence.
func TestTheUnavailableSentenceTellsASwapperWhatToDo(t *testing.T) {
	out := filepath.Join("drive", "Archive")
	full := runFatalFor(out, export.FatalFull, &export.ItemError{Path: filepath.Join(out, "p.html"), Err: errors.New("no space left on device")})
	if !strings.Contains(full.Error(), "the archive drive is full") || full.gone {
		t.Fatalf("positive twin: a full drive says %q", full.Error())
	}
	cause := errors.New("stat drive/Archive: no such file or directory")
	got := unavailable(out, cause).Error()
	want := "the archive location " + out + " is unavailable — connect the drive or network share that holds this archive (if you swapped drives, put this archive's drive back), then run the backup again. Messages written before this point are kept; nothing was recorded as archived that was not fully written. Details: stat drive/Archive: no such file or directory."
	if got != want {
		t.Fatalf("the sentence is\n%q\nwant\n%q", got, want)
	}
	// Windows ends its own words with a full stop: the sentence keeps one (the final check's
	// wording-8).
	windows := errors.New(`GetFileAttributesEx V:\Archive: The system cannot find the path specified.`)
	if got := unavailable(`V:\Archive`, windows).Error(); !strings.HasSuffix(got, `Details: GetFileAttributesEx V:\Archive: The system cannot find the path specified.`) || strings.HasSuffix(got, "..") {
		t.Fatalf("with Windows' words the sentence ends %q", got[len(got)-80:])
	}
}

// covers: MA-1460, R5
// The final check's security-2: a Cancel that lands while a proof waits, when the folder is
// already back, goes through the run's own cancel: the proof looks once more without waiting,
// the folder answers and its lock is the run's, so the backup saves its progress and its README
// before it ends cancelled, as any other cancel does. Positive twins first: a cancel with nothing
// unreadable saves too; and one while the folder still cannot be read ends cancelled at once
// (MA-1454). The verifier's probe, kept as a test: a full backup of 4 messages into a new archive,
// a proof due before every write, the lock file unreadable at the first message and readable again
// 1 s later, Cancel at 1.3 s, inside the proof's pause.
func TestACancelAfterTheFolderIsBackSavesProgress(t *testing.T) {
	ma1444Unix(t)
	old := archiveRecheckEvery
	archiveRecheckEvery = 0
	t.Cleanup(func() { archiveRecheckEvery = old })
	run := func(t *testing.T, outage bool, restoreAt, cancelAt time.Duration) (string, error) {
		_, store := imapProfile(t, "imap.a.example", nil,
			tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 2, msgs: tbMsgs("a", 2)},
			tbFolder{path: "AlphaOnly", listed: 2, msgs: tbMsgs("al", 2)})
		out := filepath.Join(tmpDir(t), "a")
		lockPath := filepath.Join(out, lockfile.Name)
		t.Cleanup(func() { _ = os.Chmod(lockPath, 0o600) })
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := false
		_, err := Run(ctx, Options{Inputs: []string{store}, Out: out, Index: true, Pages: true, Mode: export.Full}, discard(), func(export.Stats) {
			if done {
				return
			}
			done = true
			if !outage {
				cancel()
				return
			}
			_ = os.Chmod(lockPath, 0)
			if restoreAt > 0 {
				time.AfterFunc(restoreAt, func() { _ = os.Chmod(lockPath, 0o600) })
			}
			time.AfterFunc(cancelAt, cancel)
		})
		if !done {
			t.Fatal("premise: the walk was not reached")
		}
		_ = os.Chmod(lockPath, 0o600)
		return out, err
	}
	saved := func(out string) (status string, records int, readme bool) {
		lr, _, _ := state.ReadLastRun(out)
		m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
		if err == nil {
			records = m.Len()
		}
		_, rerr := os.Stat(filepath.Join(out, "README.txt"))
		return lr.Status, records, rerr == nil
	}
	out, err := run(t, false, 0, 0)
	if status, records, readme := saved(out); !errors.Is(err, context.Canceled) || status != state.RunCancelled || records < 1 || !readme {
		t.Fatalf("positive twin: a plain cancel ended %v, recorded %q, %d records, README %v", err, status, records, readme)
	}
	out, err = run(t, true, 0, 1300*time.Millisecond)
	if status, _, _ := saved(out); !errors.Is(err, context.Canceled) || status != state.RunCancelled {
		t.Fatalf("positive twin: a cancel while the folder still cannot be read ended %v, recorded %q", err, status)
	}
	out, err = run(t, true, time.Second, 1300*time.Millisecond)
	status, records, readme := saved(out)
	rc := 0
	if errors.Is(err, context.Canceled) && status == state.RunCancelled {
		rc = 130
	}
	assure.Refused(t, rc, fmt.Sprint(err), assure.Code(130), assure.Names("canceled"),
		assure.NoSideEffect(func() bool {
			if records < 1 || !readme {
				t.Logf("a cancel after the folder came back kept %d records, README %v", records, readme)
				return false
			}
			return true
		}))
}

// covers: MA-1461, R5
// The final check's wording-6: a backup whose walk could not read its own lock file for a moment
// (the walk's probe after a write fault asks the lock directly and stops on any error) used to end
// in the lock's internal sentence ("lock … was removed during the run (…): stopping so two runs
// cannot overlap; run again") when the lock turned out to be the run's after all. It now ends in
// the archive location's plain sentence, "the archive location … is unavailable — connect …", the
// system's words after "Details:", on every surface (here the error and the record). Positive twin
// first: the same backup with no fault ends ok. As an ordinary user on Unix: the store's folder made
// read-only so the next write fails, the lock file unreadable at the same moment, both put back 2 s
// later.
func TestALockReadFailureThatPassesSaysTheLocationWasUnavailable(t *testing.T) {
	ma1444Unix(t)
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 20)
	out := filepath.Join(tmpDir(t), "archive")
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out}, discard(), nil); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	tok := ma1445Token(t, out)
	storeDir := filepath.Join(out, tok)
	lockPath := filepath.Join(out, lockfile.Name)
	t.Cleanup(func() { _ = os.Chmod(storeDir, 0o755); _ = os.Chmod(lockPath, 0o600) })
	maildirMessages(t, src, 40) // 20 new: new files, which no proof before an overwrite covers
	done := false
	_, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out}, discard(), func(export.Stats) {
		if done {
			return
		}
		done = true
		_ = os.Chmod(lockPath, 0)
		_ = filepath.WalkDir(storeDir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(p, 0o555)
			}
			return nil
		})
		time.AfterFunc(2*time.Second, func() {
			_ = filepath.WalkDir(storeDir, func(p string, d fs.DirEntry, err error) error {
				if err == nil && d.IsDir() {
					_ = os.Chmod(p, 0o755)
				}
				return nil
			})
			_ = os.Chmod(lockPath, 0o600)
		})
	})
	if !done {
		t.Fatal("premise: the walk was not reached")
	}
	lr, _, _ := state.ReadLastRun(out)
	rc := 0
	if FailureKind(err) == state.FailArchiveUnwritable {
		rc = 1
	}
	assure.Refused(t, rc, fmt.Sprint(err), assure.Code(1), assure.Names("the archive location "+out+" is unavailable — connect the drive or network share that holds this archive", "Details: open "+lockPath+": permission denied."),
		assure.Forbid("stopping so two runs cannot overlap", "lock "+lockPath, "panic", "goroutine"),
		assure.NoSideEffect(func() bool { return lr.Status == state.RunFailed && lr.Error == fmt.Sprint(err) }))
}
