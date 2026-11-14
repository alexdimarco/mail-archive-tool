package app

import (
	"context"
	"io"
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
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// The 1.7.2 back-port of the archive guard (Gmail de-dup B3's adversarial rounds 3 and 4, H1 and
// J1 to J3). 1.7 keeps no archive id, so the run is bound to its folder by its lock alone. The
// refusals are in plain words, the lock's short sentence after "Details:" (the friction review's
// F1, MA-1447, and its re-walk's G1 and G2): "the archive folder changed during the backup. … then
// run the backup again. Details: the lock file was replaced (or removed) during the run."

// ma1328Bytes is the file at out/name, as bytes; "" when it is not there.
func ma1328Bytes(t *testing.T, out, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, name))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

// ma1328Top is every file directly in out (its manifest, last-run record, run history,
// BACKUP-NEEDS-ATTENTION.txt, README, report, root page, streaks, search index, lock) with its size
// and modification time: what a run of another archive must never add, remove or write again. The
// message files below it are left out: a walk stopped mid-store may have written the message in
// flight there, as after any lost lock (R5).
func ma1328Top(t *testing.T, out string) map[string]string {
	t.Helper()
	all := ma1336Files(t, out)
	for p := range all {
		if strings.Contains(p, "/") {
			delete(all, p)
		}
	}
	return all
}

// ma1328Swap renames A's folder aside and B's folder into A's place (drive 1 out, drive 2 in).
func ma1328Swap(t *testing.T, outA, aside, outB string) {
	t.Helper()
	if err := os.Rename(outA, aside); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(aside) })
	if err := os.Rename(outB, outA); err != nil {
		t.Fatal(err)
	}
}

// ma1328LockLost is the refusal's rc for assure.Refused: 2 for a run recorded lock_lost.
func ma1328LockLost(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	if FailureKind(err) == state.FailLockLost {
		return 2, err.Error()
	}
	return 1, err.Error()
}

// ma1328Untouched says the other archive, now at out, kept every file directly in its folder as
// before, and its record still says its own last backup was ok; it logs what changed.
func ma1328Untouched(t *testing.T, out string, before map[string]string) bool {
	t.Helper()
	if d := ma1336Changed(before, ma1328Top(t, out)); len(d) > 0 {
		t.Logf("written into the other archive: %v", d)
		return false
	}
	if lr, st, _ := state.ReadLastRun(out); st != state.LastRunPresent || lr.Status != state.RunOK {
		t.Logf("the other archive's record no longer says its own backup was ok: %+v (%v)", lr, st)
		return false
	}
	return true
}

// covers: MA-1328, R5
// H1: the folder comes to hold another archive after the run began, while it opens its index,
// walks its sources or has walked them (a drive of a rotation swapped at the same path; here a
// folder renamed). Before every save of its manifest, on time during its walk and before its README
// the run proves again that the lock file in its folder is still its own; here it is not, so the
// run stops as a lost lock, in the lock's words, and writes nothing into the other archive's folder:
// no manifest, record, run history entry, BACKUP-NEEDS-ATTENTION.txt, README, report or page.
// Positive twin first: without the swap the same run ends ok and saves its manifest.
func TestAFolderSwappedDuringTheWalkGetsNoManifestAndTheRunFails(t *testing.T) {
	outA, outB := tmpDir(t), tmpDir(t)
	storeA := ma1336TwoArchives(t, outA, outB)
	m0 := ma1328Bytes(t, outA, ".mailarchive-manifest.json")
	if _, err := Run(context.Background(), Options{Inputs: []string{storeA}, Out: outA, Index: true, Pages: true, Mode: export.Full}, discard(), nil); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if lr, _, _ := state.ReadLastRun(outA); lr.Status != state.RunOK || ma1328Bytes(t, outA, ".mailarchive-manifest.json") == m0 {
		t.Fatalf("positive twin: the run did not end ok with its manifest saved (%+v)", lr)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open: the drive-letter form is TestADriveLetterMovedDuringTheWalkGetsNoManifest")
	}
	for _, c := range []struct {
		name  string
		mode  export.Mode
		pause string // a pause point to swap at; "" swaps at the walk's first message
	}{
		{"during-the-presync", export.Incremental, "presync"},
		{"at-the-sweep", export.Incremental, "sweep"},
		{"at-the-index-open", export.Incremental, "index-open"},
		{"mid-walk-incremental", export.Incremental, ""},
		{"mid-walk-full", export.Full, ""},
		{"after-the-walk", export.Incremental, "walk-done"},
	} {
		t.Run(c.name, func(t *testing.T) {
			outA, outB := tmpDir(t), tmpDir(t)
			storeA := ma1336TwoArchives(t, outA, outB)
			before, all := ma1328Top(t, outB), ma1336Files(t, outB)
			swapped := false
			swap := func() {
				if !swapped {
					swapped = true
					ma1328Swap(t, outA, outA+"-drive1", outB)
				}
			}
			opts := Options{Inputs: []string{storeA}, Out: outA, Index: true, Pages: true, Mode: c.mode}
			var progress ProgressFunc
			logger := discard()
			switch c.pause {
			case "":
				progress = func(export.Stats) { swap() }
			case "presync":
				// The pre-sync waits minutes for Thunderbird, before the manifest's load: the proof
				// after the load stops the run before it opens the index or walks, so nothing at
				// all is written into the other archive, message files included.
				opts.BeforeRead = func(context.Context, []string) BeforeReadResult { swap(); return BeforeReadResult{} }
			case "sweep":
				// After the proof that follows the pre-sync and before the manifest's load: the
				// sweep of a crashed run's leftovers (it walks the whole archive) logs its count,
				// and the swap comes then. The run reads the other archive's manifest; the proof
				// after the load stops it, and nothing at all is written there, its record
				// included (the digest the run keeps of a manifest is its binding only once a
				// proof has followed the load).
				stale := filepath.Join(outA, ".mailarchive-stale.tmp")
				if err := os.WriteFile(stale, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				old := time.Now().Add(-time.Hour)
				if err := os.Chtimes(stale, old, old); err != nil {
					t.Fatal(err)
				}
				logger = log.New(&ma1337Logger{marker: "orphaned", at: swap}, "", 0)
			case "walk-done":
				afterWalkForTest = swap
				defer func() { afterWalkForTest = nil }()
			default:
				restore := OverrideRunPauseForTest(func(point string) {
					if point == c.pause {
						swap()
					}
				})
				defer restore()
			}
			_, err := Run(context.Background(), opts, logger, progress)
			if !swapped {
				t.Fatal("premise: the swap was not reached")
			}
			rc, msg := ma1328LockLost(err)
			assure.Refused(t, rc, msg, assure.Names(outA, "Details: the lock file was", "during the run", "the archive folder changed during the backup", "then run the backup again"),
				assure.NoSideEffect(func() bool {
					if (c.pause == "presync" || c.pause == "sweep") && !ma1336Untouched(t, outA, all) {
						return false
					}
					return ma1328Untouched(t, outA, before)
				}))
		})
	}
}

// covers: MA-1328, R5
// H1's Windows form: the drive letter moves to another drive's folder while the run walks its
// sources (subst stands in for a USB drive pulled and another given its letter; the lock file's
// handle stays open on drive 1). The run stops as a lost lock and drive 2's archive is left as it
// was. Positive twin: TestAFolderSwappedDuringTheWalkGetsNoManifestAndTheRunFails's, which runs
// everywhere.
func TestADriveLetterMovedDuringTheWalkGetsNoManifest(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows only (subst names a drive letter)")
	}
	const letter = "V:"
	if _, err := os.Stat(letter + `\`); err == nil {
		t.Skip(letter + " is in use on this computer")
	}
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
	storeA := ma1336TwoArchives(t, out, outB)
	before := ma1328Top(t, outB)
	swapped := false
	_, err := Run(context.Background(), Options{Inputs: []string{storeA}, Out: out, Unattended: true, Index: true, Pages: true, Mode: export.Incremental}, discard(), func(export.Stats) {
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
	assure.Refused(t, rc, msg, assure.Names("Details: the lock file was", "during the run", "the archive folder changed during the backup", "then run the backup again"),
		assure.NoSideEffect(func() bool { return ma1328Untouched(t, outB, before) }))
}

// covers: MA-1329, R5
// The check that needs no archive id: the lock is proved the run's own before every manifest
// save, not only at a checkpoint after 1,000 or more messages. Another run's lock written over this
// run's lock file while it walks a few messages (another program took the archive): the run stops
// as a lost lock and saves no manifest. Positive twin first: the same small run with its lock left
// alone saves its manifest and ends ok.
func TestALockLostDuringASmallRunSavesNoManifest(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 3)
	out := filepath.Join(tmpDir(t), "archive")
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	m0 := ma1328Bytes(t, out, ".mailarchive-manifest.json")
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Full}, discard(), nil); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	m1 := ma1328Bytes(t, out, ".mailarchive-manifest.json")
	if m1 == m0 {
		t.Fatal("positive twin: the run saved no manifest")
	}
	taken := false
	_, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Full}, discard(), func(export.Stats) {
		if !taken {
			taken = true
			if werr := os.WriteFile(filepath.Join(out, lockfile.Name), []byte("pid=1\nnonce=another-run\n"), 0o600); werr != nil {
				t.Fatal(werr)
			}
		}
	})
	if !taken {
		t.Fatal("premise: the walk was not reached")
	}
	rc, msg := ma1328LockLost(err)
	assure.Refused(t, rc, msg, assure.Names(lockfile.Name, "was replaced during the run", "was changed during the backup", "Run the backup again once any other backup of it has finished"),
		assure.NoSideEffect(func() bool { return ma1328Bytes(t, out, ".mailarchive-manifest.json") == m1 }))
}

// covers: MA-1330, R5
// While the run walks, the folder is proved to be the run's on time too, not only before a save:
// a walk that reads for minutes stops writing message files into a folder that came to hold
// another archive within one message of the next proof. Here the proof is due at every message
// (the interval made 0): after the swap the walk reads at most one more message. Positive twin
// first: with the proof only before the saves (the interval long), the walk goes on through the
// store after the swap.
func TestTheWalkStopsSoonAfterItsFolderHoldsAnotherArchive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open")
	}
	after := func(every time.Duration) int {
		_, storeA := imapProfile(t, "imap.a.example", nil,
			tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 6, msgs: tbMsgs("a", 6)},
			tbFolder{path: "AlphaOnly", listed: 6, msgs: tbMsgs("al", 6)})
		_, storeB := imapProfile(t, "imap.b.example", nil,
			tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 2, msgs: tbMsgs("b", 2)})
		outA, outB := tmpDir(t), tmpDir(t)
		for _, r := range []struct{ store, out string }{{storeA, outA}, {storeB, outB}} {
			if _, err := Run(context.Background(), Options{Inputs: []string{r.store}, Out: r.out}, discard(), nil); err != nil {
				t.Fatal(err)
			}
		}
		old := archiveRecheckEvery
		archiveRecheckEvery = every
		defer func() { archiveRecheckEvery = old }()
		swapped, n := false, 0
		_, _ = Run(context.Background(), Options{Inputs: []string{storeA}, Out: outA, Mode: export.Full}, discard(), func(export.Stats) {
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
		return n
	}
	if n := after(time.Hour); n < 3 {
		t.Fatalf("positive twin: with no proof on time the walk read only %d messages after the swap", n)
	}
	if n := after(0); n > 1 {
		t.Fatalf("the walk read %d messages after its folder came to hold another archive", n)
	}
}

// covers: MA-1328, R5
// H1 for a Microsoft 365 capture: the folder comes to hold another archive while the capture lists
// a mailbox. Before each save the capture proves its lock is still its own; here it is not, so it
// stops as a lost lock and writes nothing into the other archive's folder. Positive twin first:
// without the swap the capture ends ok.
func TestACaptureWhoseFolderIsSwappedWritesNothingIntoTheOther(t *testing.T) {
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
	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"}, BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	logger := log.New(io.Discard, "", 0)
	capture := func(out string) error {
		_, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}, logger)
		return err
	}
	outA := filepath.Join(tmpDir(t), "archive")
	if err := capture(outA); err != nil {
		t.Fatal(err)
	}
	if err := capture(outA); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open")
	}
	outB := filepath.Join(tmpDir(t), "archive")
	if err := capture(outB); err != nil {
		t.Fatal(err)
	}
	before := ma1328Top(t, outB)
	swapped := false
	mu.Lock()
	onList = func() { swapped = true; ma1328Swap(t, outA, outA+"-drive1", outB) }
	mu.Unlock()
	err := capture(outA)
	if !swapped {
		t.Fatal("premise: the listing was not reached")
	}
	rc, msg := ma1328LockLost(err)
	assure.Refused(t, rc, msg, assure.Names(outA, "Details: the lock file was", "during the run", "the archive folder changed during the backup", "then run the backup again"),
		assure.NoSideEffect(func() bool { return ma1328Untouched(t, outA, before) }))
}
