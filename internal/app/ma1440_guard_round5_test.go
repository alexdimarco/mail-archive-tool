package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/pages"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// The Gmail de-dup B3 adversarial pass, round 5's K1 to K4, folded into the 1.7.2 back-port of
// the archive guard (docs/review-gmail-dedup-b3-adversarial-r5.md on wip/gmail-dedup-b3-adv5).

// ma1440RenameRecords gives every record of the plaintext archive at out an older file name
// ("old-<name>"), on disk and in its manifest, as an earlier naming rule would have named them,
// so the archive's next Full run re-exports every message under a new name and removes the old
// file. It returns the old names.
func ma1440RenameRecords(t *testing.T, out string) []string {
	t.Helper()
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var olds []string
	for key, rec := range m.All() {
		if rec.Path == "" || !strings.HasSuffix(rec.Path, ".html") {
			continue
		}
		oldRel := filepath.ToSlash(filepath.Join(filepath.Dir(filepath.FromSlash(rec.Path)), "old-"+filepath.Base(rec.Path)))
		if err := os.Rename(filepath.Join(out, filepath.FromSlash(rec.Path)), filepath.Join(out, filepath.FromSlash(oldRel))); err != nil {
			t.Fatal(err)
		}
		rec.Path = oldRel
		m.Add(key, rec)
		olds = append(olds, oldRel)
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	return olds
}

// covers: MA-1440, R5
// K1: a plaintext walk proves on time (every 10 seconds) before it writes a new file, but right
// before it writes over a file already in the folder (a Full run, a re-capture, a fill) or removes
// one (a message re-exported under a new name), it proves at once: those are the changes the other
// archive's own next backup could not undo. Here A and B are two drives of one mailbox, the proof
// on time never due (an hour), and B swapped into A's place after A's first message. "renamed":
// both drives' records carry an older file name, so A's Full run removes each old file after
// writing the new one; "written-over": A's Full run writes every page again at the same path. In
// both, no file B had is removed or written again (the new-named page of the message in flight
// may be added: the on-time bound), and A's run stops as a lost lock. Positive twin first: A's
// Full run without the swap re-exports every message under its new name and removes every
// old-named file.
func TestAReplaceOrRemovalAfterASwapIsProvedFirst(t *testing.T) {
	old := archiveRecheckEvery
	archiveRecheckEvery = time.Hour
	t.Cleanup(func() { archiveRecheckEvery = old })
	twoDrives := func(t *testing.T) (store, outA, outB string) {
		t.Helper()
		_, store = imapProfile(t, "imap.k1.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 6, msgs: tbMsgs("k1", 6)})
		outA, outB = tmpDir(t), tmpDir(t)
		for _, o := range []string{outA, outB} {
			if _, err := Run(context.Background(), Options{Inputs: []string{store}, Out: o}, discard(), nil); err != nil {
				t.Fatalf("first backup into %s: %v", o, err)
			}
		}
		return store, outA, outB
	}

	store, outA, _ := twoDrives(t)
	olds := ma1440RenameRecords(t, outA)
	if _, err := Run(context.Background(), Options{Inputs: []string{store}, Out: outA, Mode: export.Full}, discard(), nil); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	for _, rel := range assure.Reached(t, olds, "the old-named files") {
		if _, err := os.Stat(filepath.Join(outA, filepath.FromSlash(rel))); err == nil {
			t.Fatalf("positive twin: the re-export left its old-named file %s", rel)
		}
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open")
	}
	for _, c := range []string{"renamed", "written-over"} {
		t.Run(c, func(t *testing.T) {
			store, outA, outB := twoDrives(t)
			if c == "renamed" {
				ma1440RenameRecords(t, outA)
				ma1440RenameRecords(t, outB)
			}
			before := ma1336Files(t, outB)
			swapped := false
			_, err := Run(context.Background(), Options{Inputs: []string{store}, Out: outA, Mode: export.Full}, discard(), func(export.Stats) {
				if !swapped {
					swapped = true
					ma1328Swap(t, outA, outA+"-drive1", outB)
				}
			})
			if !swapped {
				t.Fatal("premise: the walk was not reached")
			}
			rc, msg := ma1328LockLost(err)
			assure.Refused(t, rc, msg, assure.Names(filepath.Join(outA, lockfile.Name), "during the run", "run again"),
				assure.NoSideEffect(func() bool {
					var lost []string
					for _, d := range ma1336Changed(before, ma1336Files(t, outA)) {
						if !strings.HasPrefix(d, "added ") {
							lost = append(lost, d)
						}
					}
					if len(lost) > 0 {
						t.Logf("B's own files removed or written again: %v", lost)
						return false
					}
					return true
				}))
		})
	}
}

// covers: MA-1440, R5
// K1's Windows form: the drive letter moves to another drive's folder (subst stands in for a USB
// drive pulled and another given its letter) after the first message of a Full run that
// re-exports every message under a new name; the other drive's own old-named files are neither
// removed nor written again. Positive twin: TestAReplaceOrRemovalAfterASwapIsProvedFirst's, which
// runs everywhere.
func TestADriveLetterMovedBeforeAReplaceGetsNothingReplaced(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows only (subst names a drive letter)")
	}
	const letter = "T:"
	if _, err := os.Stat(letter + `\`); err == nil {
		t.Skip(letter + " is in use on this computer")
	}
	old := archiveRecheckEvery
	archiveRecheckEvery = time.Hour
	t.Cleanup(func() { archiveRecheckEvery = old })
	drive1, drive2 := t.TempDir(), t.TempDir()
	subst := func(args ...string) {
		if out, err := exec.Command("subst", args...).CombinedOutput(); err != nil {
			t.Fatalf("subst %v: %v %s", args, err, out)
		}
	}
	subst(letter, drive1)
	t.Cleanup(func() { _ = exec.Command("subst", letter, "/D").Run() })
	out := letter + `\MailArchive`
	outB := filepath.Join(drive2, "MailArchive")
	_, store := imapProfile(t, "imap.k1w.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 6, msgs: tbMsgs("k1w", 6)})
	for _, o := range []string{out, outB} {
		if _, err := Run(context.Background(), Options{Inputs: []string{store}, Out: o}, discard(), nil); err != nil {
			t.Fatalf("first backup into %s: %v", o, err)
		}
		ma1440RenameRecords(t, o)
	}
	before := ma1336Files(t, outB)
	swapped := false
	_, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Mode: export.Full}, discard(), func(export.Stats) {
		if !swapped {
			swapped = true
			subst(letter, "/D")
			subst(letter, drive2)
		}
	})
	if !swapped {
		t.Fatal("premise: the walk was not reached")
	}
	rc, msg := ma1328LockLost(err)
	assure.Refused(t, rc, msg, assure.Names(lockfile.Name, "during the run", "run again"),
		assure.NoSideEffect(func() bool {
			var lost []string
			for _, d := range ma1336Changed(before, ma1336Files(t, outB)) {
				if !strings.HasPrefix(d, "added ") {
					lost = append(lost, d)
				}
			}
			if len(lost) > 0 {
				t.Logf("drive 2's own files removed or written again: %v", lost)
				return false
			}
			return true
		}))
}

// covers: MA-1440, R5
// K1 for a Microsoft 365 capture: a Full capture writes every message page again at its path,
// and after a swap at its first listing (the proof on time never due) those pages are B's. The
// capture proves right before writing over each file already there, so none of B's files is
// written again or removed, and it stops as a lost lock. Positive twin first: the same Full
// capture without the swap ends ok and writes A's pages again.
func TestACapturesReplaceAfterASwapIsProvedFirst(t *testing.T) {
	old := archiveRecheckEvery
	archiveRecheckEvery = time.Hour
	t.Cleanup(func() { archiveRecheckEvery = old })
	_, inner := newFakeGraphServer()
	defer inner.Close()
	var mu sync.Mutex
	var onList func()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			mu.Lock()
			f := onList
			onList = nil
			mu.Unlock()
			if f != nil {
				f()
			}
		}
		inner.Config.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	capture := ma1337Capture(t, srv.URL)
	full := Options{Mode: export.Full, Index: true, Pages: true}
	outA := filepath.Join(tmpDir(t), "archive")
	if err := capture(outA, full, discard()); err != nil {
		t.Fatal(err)
	}
	beforeA := ma1336Files(t, outA)
	if err := capture(outA, full, discard()); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	rewritten := 0
	for _, d := range ma1336Changed(beforeA, ma1336Files(t, outA)) {
		if strings.HasPrefix(d, "written ") && strings.HasSuffix(d, ".html") {
			rewritten++
		}
	}
	if rewritten == 0 {
		t.Fatal("positive twin: the Full capture wrote no page again")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open")
	}
	outB := filepath.Join(tmpDir(t), "archive")
	if err := capture(outB, full, discard()); err != nil {
		t.Fatal(err)
	}
	before := ma1336Files(t, outB)
	swapped := false
	mu.Lock()
	onList = func() { swapped = true; ma1328Swap(t, outA, outA+"-drive1", outB) }
	mu.Unlock()
	err := capture(outA, full, discard())
	if !swapped {
		t.Fatal("premise: the listing was not reached")
	}
	rc, msg := ma1328LockLost(err)
	assure.Refused(t, rc, msg, assure.Names(filepath.Join(outA, lockfile.Name), "during the run", "run again"),
		assure.NoSideEffect(func() bool {
			var lost []string
			for _, d := range ma1336Changed(before, ma1336Files(t, outA)) {
				if !strings.HasPrefix(d, "added ") {
					lost = append(lost, d)
				}
			}
			if len(lost) > 0 {
				t.Logf("B's own files removed or written again: %v", lost)
				return false
			}
			return true
		}))
}

// ma1441Index is a search index holding one message in each of folders, for the pages.
func ma1441Index(t *testing.T, folders ...string) *index.Index {
	t.Helper()
	ix, err := index.Open(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	for i, folder := range folders {
		m := &model.Message{Subject: "Subject-" + folder, SenderName: "S", Received: time.Date(2025, 1, 1+i, 9, 0, 0, 0, time.UTC), PlainBody: "b"}
		if err := ix.Add("store", []string{folder}, m, fmt.Sprintf("store/%s/m.html", folder), folder+"\x00id"); err != nil {
			t.Fatal(err)
		}
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}
	return ix
}

// covers: MA-1441, R5
// K2: the tail's folder pages are each written over the page of the same folder in whatever
// archive the folder holds, so the guard proves before every page, not only on time: writing a
// few hundred pages takes far less than 10 seconds, so on time meant no proof between the first
// page and the root page. Here the proof on time is never due (an hour) and the lock is lost once
// the first folder page is written (the guard's own lock question answers no from then on): only
// that page is written, no other folder page and no root page, and the proof's answer is
// returned. Positive twin first: with the lock kept, every page is written and the lock is asked
// about before each one (and once before the first).
func TestEveryFolderPageIsProvedBeforeItIsWritten(t *testing.T) {
	old := archiveRecheckEvery
	archiveRecheckEvery = time.Hour
	t.Cleanup(func() { archiveRecheckEvery = old })
	ix := ma1441Index(t, "Alpha", "Beta", "Gamma")
	pagesIn := func(out string) []string {
		var got []string
		for _, rel := range []string{"store/Alpha/index.html", "store/Beta/index.html", "store/Gamma/index.html", pages.RootPage} {
			if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err == nil {
				got = append(got, rel)
			}
		}
		return got
	}

	all, asked := t.TempDir(), 0
	g := &archiveGuard{out: all, stillHeld: func() error { asked++; return nil }}
	if err := g.writePages(ix, discard(), 0); err != nil || len(pagesIn(all)) != 4 || asked != 5 {
		t.Fatalf("positive twin: %v; pages %v; the lock asked %d times, want 5 (once first, then before each of 4 pages)", err, pagesIn(all), asked)
	}

	out, calls := t.TempDir(), 0
	lost := errors.New("lock " + filepath.Join(out, lockfile.Name) + " was replaced during the run: stopping so two runs cannot overlap; run again")
	g = &archiveGuard{out: out, stillHeld: func() error {
		calls++
		if calls > 2 { // the first proof, then the first page's: the swap comes after that page
			return lost
		}
		return nil
	}}
	err := g.writePages(ix, discard(), 0)
	rc := 0
	if err != nil {
		rc = 2
	}
	assure.Refused(t, rc, fmt.Sprint(err), assure.Names("was replaced during the run", "run again"),
		assure.NoSideEffect(func() bool {
			got := pagesIn(out)
			if !errors.Is(err, lost) || len(got) != 1 {
				t.Logf("pages written: %v", got)
				return false
			}
			return true
		}))
}

// ma1442Encrypted is whether c names the encrypted form.
func ma1442Encrypted(c string) bool { return strings.HasPrefix(c, "encrypted") }

// covers: MA-1442, R5
// K3: a run whose lock is no longer its own cannot tell, by the lock alone, a swapped-in archive
// from its own archive whose lock file was removed (a person, a cleaning or sync tool) or written
// over. So its record asks whether the folder's manifest (an encrypted archive's record pointer)
// still holds the bytes the run last read or wrote. The same: the folder is still this archive,
// and the run is recorded "failed" there with its lost lock, as 1.7.1 recorded it: the record
// says so, a run history entry is added and BACKUP-NEEDS-ATTENTION.txt written (never "never
// finished … crash, kill, or power loss"). Different (another run took the lock and saved since):
// nothing is recorded (the swapped-in archive's case is MA-1328 and MA-1338). Forms: plaintext
// and encrypted, the lock file written over (every system) or removed (not on Windows, which
// refuses to remove an open file). Positive twin first: the same run with its lock left alone
// ends ok.
func TestALockLostInItsOwnArchiveIsRecordedThere(t *testing.T) {
	src := encryptedSourcePST(t)
	second := filepath.Join(tmpDir(t), "Second")
	maildirMessages(t, second, 3)
	archive := func(t *testing.T, enc bool, inputs ...string) Options {
		t.Helper()
		opts := Options{Inputs: append([]string{src}, inputs...), Mode: export.Full}
		if enc {
			opts.Out, opts.Key = encryptedOut(t)
		} else {
			opts.Out = tmpDir(t)
		}
		if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
			t.Fatalf("first backup: %v", err)
		}
		return opts
	}
	history := func(t *testing.T, out string) int {
		h, err := state.ReadRunHistory(out)
		if err != nil {
			t.Fatalf("run history: %v", err)
		}
		return len(h)
	}
	for _, enc := range []bool{false, true} {
		opts := archive(t, enc)
		if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
			t.Fatalf("positive twin (encrypted %v): %v", enc, err)
		}
	}
	// "-after-a-save": the run has two sources and its lock is written over once the second one
	// starts, after the first one's save: the bytes compared are those the run wrote, not read.
	cases := []string{"plaintext-written-over", "encrypted-written-over", "plaintext-after-a-save", "encrypted-after-a-save",
		"plaintext-another-run-saved", "encrypted-another-run-saved"}
	if runtime.GOOS != "windows" {
		cases = append(cases, "plaintext-removed", "encrypted-removed")
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			var opts Options
			if strings.HasSuffix(c, "-after-a-save") {
				opts = archive(t, ma1442Encrypted(c), second)
			} else {
				opts = archive(t, ma1442Encrypted(c))
			}
			lockPath := filepath.Join(opts.Out, lockfile.Name)
			histBefore := history(t, opts.Out)
			done := false
			act := func() {
				if done {
					return
				}
				done = true
				if strings.HasSuffix(c, "-removed") {
					if rerr := os.Remove(lockPath); rerr != nil {
						t.Fatal(rerr)
					}
					return
				}
				if werr := os.WriteFile(lockPath, []byte("pid=1\nnonce=another-run\n"), 0o600); werr != nil {
					t.Fatal(werr)
				}
				if strings.HasSuffix(c, "-another-run-saved") {
					// The other run's save: the folder's record file is no longer what this run read.
					rec := filepath.Join(opts.Out, ".mailarchive-manifest.json")
					if ma1442Encrypted(c) {
						rec = filepath.Join(opts.Out, container.PointerName)
					}
					f, ferr := os.OpenFile(rec, os.O_APPEND|os.O_WRONLY, 0)
					if ferr != nil {
						t.Fatal(ferr)
					}
					_, _ = f.WriteString(" ")
					f.Close()
				}
			}
			logger, progress := discard(), ProgressFunc(func(export.Stats) { act() })
			if strings.HasSuffix(c, "-after-a-save") {
				// The second source's "Reading …" line: the first source is saved by then.
				logger, progress = log.New(&ma1337Logger{marker: "Reading " + second, at: act}, "", 0), nil
			}
			_, err := Run(context.Background(), opts, logger, progress)
			if !done {
				t.Fatal("premise: the walk was not reached")
			}
			lr, st, _ := state.ReadLastRun(opts.Out)
			_, noticeErr := os.Stat(filepath.Join(opts.Out, state.AttentionName))
			rc, msg := ma1328LockLost(err)
			if strings.HasSuffix(c, "-another-run-saved") {
				assure.Refused(t, rc, msg, assure.Names(lockfile.Name, "during the run", "run again"),
					assure.NoSideEffect(func() bool {
						if st != state.LastRunPresent || lr.Status != state.RunRunning || history(t, opts.Out) != histBefore || noticeErr == nil {
							t.Logf("recorded in a folder another run saved since: record %q, history %d -> %d, notice %v", lr.Status, histBefore, history(t, opts.Out), noticeErr == nil)
							return false
						}
						return true
					}))
				return
			}
			assure.Refused(t, rc, msg, assure.Names(lockfile.Name, "during the run", "run again"))
			if st != state.LastRunPresent || lr.Status != state.RunFailed {
				t.Fatalf("the run's own archive was not recorded failed: %q (%v)", lr.Status, st)
			}
			if ma1442Encrypted(c) && lr.ErrorKind != state.FailLockLost {
				t.Errorf("the encrypted record's kind is %q, want %q", lr.ErrorKind, state.FailLockLost)
			}
			if !ma1442Encrypted(c) && !strings.Contains(lr.Error, lockfile.Name) {
				t.Errorf("the record's words do not name the lock: %q", lr.Error)
			}
			if h := history(t, opts.Out); h != histBefore+1 {
				t.Errorf("run history %d -> %d, want one entry more", histBefore, h)
			}
			if noticeErr != nil {
				t.Errorf("no %s: %v", state.AttentionName, noticeErr)
			}
		})
	}
}

// covers: MA-1443, R5
// K4: an encrypted run's manifest is loaded before the Thunderbird pre-sync's wait, so the proof
// that follows a plaintext run's manifest load never ran for it, and its first proof after the
// wait came after its first message was sealed into whatever archive the folder held. Every run
// now proves right after the pre-sync, before its sweeps or anything else: an encrypted archive
// swapped in during the wait gets nothing at all, not one file added, removed or written again,
// and its record still says its own last backup was ok. Positive twin first: the same run with a
// pre-sync hook and no swap ends ok.
func TestAnEncryptedRunSwappedDuringItsPresyncWritesNothing(t *testing.T) {
	src := encryptedSourcePST(t)
	noSync := func(context.Context, []string) BeforeReadResult { return BeforeReadResult{} }
	outT, kT := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: outT, Key: kT, BeforeRead: noSync}, discard(), nil); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open")
	}
	outA, kA := encryptedOut(t)
	outB, kB := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: outB, Key: kB}, discard(), nil); err != nil {
		t.Fatalf("B's own backup: %v", err)
	}
	before := ma1336Files(t, outB)
	swapped := false
	opts := Options{Inputs: []string{src}, Out: outA, Key: kA, Mode: export.Full}
	opts.BeforeRead = func(context.Context, []string) BeforeReadResult {
		swapped = true
		ma1328Swap(t, outA, outA+"-drive1", outB)
		return BeforeReadResult{}
	}
	_, err := Run(context.Background(), opts, discard(), nil)
	if !swapped {
		t.Fatal("premise: the pre-sync was not reached")
	}
	rc, msg := ma1328LockLost(err)
	assure.Refused(t, rc, msg, assure.Names(lockfile.Name, "during the run", "run again"),
		assure.NoSideEffect(func() bool {
			if !ma1336Untouched(t, outA, before) {
				return false
			}
			lr, st, _ := state.ReadLastRun(outA)
			return st == state.LastRunPresent && lr.Status == state.RunOK
		}))
}
