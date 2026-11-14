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
)

// ma1328Bytes is the file at out/name, as bytes; "" when it is not there.
func ma1328Bytes(t *testing.T, out, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, name))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

// ma1328Untouched is what the archive at out holds that a run of another archive must never
// write: its manifest, its last-run record and its README.
type ma1328Untouched struct{ manifest, record, readme string }

func ma1328Snapshot(t *testing.T, out string) ma1328Untouched {
	t.Helper()
	return ma1328Untouched{
		manifest: ma1328Bytes(t, out, ".mailarchive-manifest.json"),
		record:   ma1328Bytes(t, out, state.LastRunName),
		readme:   ma1328Bytes(t, out, "README.txt"),
	}
}

// ma1328Swap renames A's folder aside and B's folder into A's place (drive 1 out, drive 2 in).
func ma1328Swap(t *testing.T, outA, aside, outB string) {
	t.Helper()
	if err := os.Rename(outA, aside); err != nil {
		t.Fatal(err)
	}
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

// covers: MA-1328, R5, R45
// The B3 adversarial pass, round 3's H1: the folder comes to hold another archive after the check
// that follows the manifest's load, while the run opens its index, walks its sources or has walked
// them (a drive of a rotation swapped at the same path; here a folder renamed). Before every save
// of its manifest, and before its README, the run proves again that the folder holds the archive it
// began with and that the lock is still its own; here it does not, so the run stops as a lost lock,
// with words that say so, and writes no manifest, record or README into the other archive, and the
// yes kept for A never reaches B. Positive twin first: without the swap the same run switches A and
// ends ok.
func TestAFolderSwappedDuringTheWalkGetsNoManifestAndTheRunFails(t *testing.T) {
	ma1316Settings(t)
	_, outA, _, twin := ma1316YesFor(t)
	twin.Index = true
	if _, err := Run(context.Background(), twin, discard(), nil); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if v, on := manifestOnDisk(t, outA); v != 7 || len(on) == 0 {
		t.Fatalf("positive twin: the yes was not applied to A (version %d, on %v)", v, on)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open: the drive-letter form is TestADriveLetterMovedDuringTheWalkGetsNoManifest")
	}
	for _, c := range []struct {
		name  string
		mode  export.Mode
		pause string // a pause point to swap at; "" swaps at the walk's first message
	}{
		{"at-the-index-open", export.Incremental, "index-open"},
		{"mid-walk-incremental", export.Incremental, ""},
		{"mid-walk-full", export.Full, ""},
		{"after-the-walk", export.Incremental, "walk-done"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ma1316Settings(t)
			storeA, outA, _, opts := ma1316YesFor(t)
			opts.Mode, opts.Index = c.mode, true
			outB := ma1316OtherArchive(t, storeA)
			before := ma1328Snapshot(t, outB)
			aside := outA + "-drive1"
			swapped := false
			swap := func() {
				if !swapped {
					swapped = true
					ma1328Swap(t, outA, aside, outB)
				}
			}
			var progress ProgressFunc
			switch c.pause {
			case "":
				progress = func(export.Stats) { swap() }
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
			_, err := Run(context.Background(), opts, discard(), progress)
			if !swapped {
				t.Fatal("premise: the swap was not reached")
			}
			rc, msg := ma1328LockLost(err)
			assure.Refused(t, rc, msg, assure.Names(outA, "another archive", "Run the backup again"),
				assure.NoSideEffect(func() bool {
					v, on := manifestOnDisk(t, outA)
					return ma1328Snapshot(t, outA) == before && v == 6 && len(on) == 0
				}))
		})
	}
}

// covers: MA-1328, R5, R45
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
	ma1316Settings(t)
	drive1, drive2 := t.TempDir(), t.TempDir()
	subst := func(args ...string) {
		if out, err := exec.Command("subst", args...).CombinedOutput(); err != nil {
			t.Fatalf("subst %v: %v %s", args, err, out)
		}
	}
	subst(letter, drive1)
	t.Cleanup(func() { _ = exec.Command("subst", letter, "/D").Run() })
	out := letter + `\MailArchive`
	_, store := imapProfile(t, "imap.gmail.com", map[string]string{"is_gmail": "true"}, gmailFolders(6)...)
	gmailDetectOff = true
	runTB(t, Options{Inputs: []string{store}, Out: out})
	gmailDetectOff = false
	outB := filepath.Join(drive2, "MailArchive")
	gmailDetectOff = true
	runTB(t, Options{Inputs: []string{store}, Out: outB})
	gmailDetectOff = false
	before := ma1328Snapshot(t, outB)
	swapped := false
	opts := Options{Inputs: []string{store}, Out: out, Unattended: true, Mode: export.Incremental, GmailYes: true}
	_, err := Run(context.Background(), opts, discard(), func(export.Stats) {
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
	assure.Refused(t, rc, msg, assure.Names("another archive", "Run the backup again"),
		assure.NoSideEffect(func() bool {
			v, on := manifestOnDisk(t, outB)
			return ma1328Snapshot(t, outB) == before && v == 6 && len(on) == 0
		}))
}

// covers: MA-1329, R5
// The same check without an archive id (the part a release before 1.8, which keeps no id, can
// take): the lock is proved the run's own before every manifest save, not only at a checkpoint
// after 1,000 or more messages. Another run's lock written over this run's lock file while it
// walks a few messages (another program took the archive): the run stops as a lost lock and saves
// no manifest. Positive twin first: the same small run with its lock left alone saves its manifest
// and ends ok.
func TestALockLostDuringASmallRunSavesNoManifest(t *testing.T) {
	ma1316Settings(t)
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
	assure.Refused(t, rc, msg, assure.Names(lockfile.Name, "run again"),
		assure.NoSideEffect(func() bool { return ma1328Bytes(t, out, ".mailarchive-manifest.json") == m1 }))
}

// covers: MA-1330, R5, R45
// While the run walks, the folder is proved to hold the run's archive on time too, not only before
// a save: a walk that reads for minutes stops writing message files into a folder that came to
// hold another archive within one message of the next proof. Here the proof is due at every
// message (the interval made 0): after the swap the walk reads at most one more message. Positive
// twin first: with the proof only before the saves (the interval long), the walk goes on through
// the store after the swap.
func TestTheWalkStopsSoonAfterItsFolderHoldsAnotherArchive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open")
	}
	after := func(every time.Duration) int {
		ma1316Settings(t)
		storeA, outA := existingGmailArchive(t)
		outB := ma1316OtherArchive(t, storeA)
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
// a mailbox. Before each save the capture proves the folder still holds its archive and the lock is
// its own; here it does not, so it stops as a lost lock and writes no manifest, record or README
// into the other archive. Positive twin first: without the swap the capture ends ok.
func TestACaptureWhoseFolderIsSwappedWritesNothingIntoTheOther(t *testing.T) {
	ma1316Settings(t)
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
	before := ma1328Snapshot(t, outB)
	swapped := false
	mu.Lock()
	onList = func() { swapped = true; ma1328Swap(t, outA, outA+"-drive1", outB) }
	mu.Unlock()
	err := capture(outA)
	if !swapped {
		t.Fatal("premise: the listing was not reached")
	}
	rc, msg := ma1328LockLost(err)
	assure.Refused(t, rc, msg, assure.Names(outA, "another archive", "Run the backup again"),
		assure.NoSideEffect(func() bool { return ma1328Snapshot(t, outA) == before }))
}
