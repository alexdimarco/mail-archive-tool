package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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

// ma1336Files is every file under out (archive-relative) with its size and modification time, so
// a file written again with the same bytes is a change too.
func ma1336Files(t *testing.T, out string) map[string]string {
	t.Helper()
	m := map[string]string{}
	_ = filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		fi, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		rel, _ := filepath.Rel(out, p)
		m[filepath.ToSlash(rel)] = fmt.Sprintf("%s %d", fi.ModTime().Format(time.RFC3339Nano), fi.Size())
		return nil
	})
	return m
}

// ma1336Changed names what differs between two listings: added, removed or written again.
func ma1336Changed(before, after map[string]string) []string {
	var d []string
	for p, m := range after {
		if b, ok := before[p]; !ok {
			d = append(d, "added "+p)
		} else if b != m {
			d = append(d, "written "+p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			d = append(d, "removed "+p)
		}
	}
	sort.Strings(d)
	return d
}

// ma1336TwoArchives makes archive A (folders INBOX and AlphaOnly) and archive B (INBOX and
// BetaOnly), each from its own Thunderbird store, with a search index and folder pages.
func ma1336TwoArchives(t *testing.T, outA, outB string) (storeA string) {
	t.Helper()
	_, storeA = imapProfile(t, "imap.a.example", nil,
		tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 2, msgs: tbMsgs("a", 2)},
		tbFolder{path: "AlphaOnly", listed: 2, msgs: tbMsgs("al", 2)})
	_, storeB := imapProfile(t, "imap.b.example", nil,
		tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 3, msgs: tbMsgs("b", 3)},
		tbFolder{path: "BetaOnly", listed: 1, msgs: tbMsgs("be", 1)})
	for _, r := range []struct{ store, out string }{{storeA, outA}, {storeB, outB}} {
		if _, err := Run(context.Background(), Options{Inputs: []string{r.store}, Out: r.out, Index: true, Pages: true, Mode: export.Incremental}, discard(), nil); err != nil {
			t.Fatalf("first backup into %s: %v", r.out, err)
		}
	}
	return storeA
}

// ma1336Untouched says nothing at all in the other archive, now at out, was added, removed or
// written again; it logs what was.
func ma1336Untouched(t *testing.T, out string, before map[string]string) bool {
	t.Helper()
	if d := ma1336Changed(before, ma1336Files(t, out)); len(d) > 0 {
		t.Logf("written into the other archive: %v", d)
		return false
	}
	return true
}

// covers: MA-1336, R5
// J1: the folder comes to hold another archive after the proof that follows the walk, in the run's
// tail, which writes the folder pages, the root index.html, the README and the report by path. The
// tail proves again before each of those writes (and on time between pages), so the run stops as a
// lost lock, in the lock's words, and writes nothing at all into the other archive. Both batches: a
// full run, whose pages are written again, and an incremental run with nothing new, whose pages are
// not, so its README comes first. Positive twin first: without the swap the full run ends ok and
// writes A's pages again.
func TestAFolderSwappedInTheRunsTailGetsNoPagesOrReadme(t *testing.T) {
	outA, outB := tmpDir(t), tmpDir(t)
	storeA := ma1336TwoArchives(t, outA, outB)
	rootA := ma1328Bytes(t, outA, "index.html")
	before := ma1336Files(t, outA)
	if _, err := Run(context.Background(), Options{Inputs: []string{storeA}, Out: outA, Index: true, Pages: true, Mode: export.Full}, discard(), nil); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if after := ma1336Files(t, outA); after["index.html"] == before["index.html"] || !strings.Contains(rootA, "AlphaOnly") {
		t.Fatal("positive twin: the full run did not write A's root page again")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open: the drive-letter form is TestADriveLetterMovedInTheRunsTailGetsNoPages")
	}
	for _, mode := range []export.Mode{export.Full, export.Incremental} {
		t.Run(map[export.Mode]string{export.Full: "pages-written-again", export.Incremental: "nothing-new"}[mode], func(t *testing.T) {
			outA, outB := tmpDir(t), tmpDir(t)
			storeA := ma1336TwoArchives(t, outA, outB)
			before := ma1336Files(t, outB)
			swapped := false
			tailForTest = func() {
				if !swapped {
					swapped = true
					ma1328Swap(t, outA, outA+"-drive1", outB)
				}
			}
			defer func() { tailForTest = nil }()
			_, err := Run(context.Background(), Options{Inputs: []string{storeA}, Out: outA, Index: true, Pages: true, Mode: mode}, discard(), nil)
			if !swapped {
				t.Fatal("premise: the tail was not reached")
			}
			rc, msg := ma1328LockLost(err)
			assure.Refused(t, rc, msg, assure.Names(filepath.Join(outA, lockfile.Name), "during the run", "run again"),
				assure.NoSideEffect(func() bool { return ma1336Untouched(t, outA, before) }))
		})
	}
}

// covers: MA-1336, R5
// J1's Windows form: the drive letter moves to another drive's folder in the run's tail (subst
// stands in for a USB drive pulled and another given its letter). The run stops as a lost lock and
// drive 2's archive gets none of drive 1's pages, README or report. Positive twin:
// TestAFolderSwappedInTheRunsTailGetsNoPagesOrReadme's, which runs everywhere.
func TestADriveLetterMovedInTheRunsTailGetsNoPages(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows only (subst names a drive letter)")
	}
	const letter = "U:"
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
	before := ma1336Files(t, outB)
	swapped := false
	tailForTest = func() {
		if !swapped {
			swapped = true
			subst(letter, "/D")
			subst(letter, drive2)
		}
	}
	defer func() { tailForTest = nil }()
	_, err := Run(context.Background(), Options{Inputs: []string{storeA}, Out: out, Index: true, Pages: true, Mode: export.Full}, discard(), nil)
	if !swapped {
		t.Fatal("premise: the tail was not reached")
	}
	rc, msg := ma1328LockLost(err)
	assure.Refused(t, rc, msg, assure.Names(lockfile.Name, "during the run", "run again"),
		assure.NoSideEffect(func() bool { return ma1336Untouched(t, outB, before) }))
}

// ma1337Logger is a capture's logger that calls at once when a line holding marker is written: a
// seam made of the capture's own log lines.
type ma1337Logger struct {
	mu     sync.Mutex
	marker string
	at     func()
}

func (w *ma1337Logger) Write(p []byte) (int, error) {
	w.mu.Lock()
	f := w.at
	if f != nil && strings.Contains(string(p), w.marker) {
		w.at = nil
	} else {
		f = nil
	}
	w.mu.Unlock()
	if f != nil {
		f()
	}
	return len(p), nil
}

// ma1337Capture is a Microsoft 365 capture of mailbox u1 from the package's fake Graph server.
func ma1337Capture(t *testing.T, srvURL string) func(out string, opts Options, logger *log.Logger) error {
	t.Helper()
	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"}, BaseURL: srvURL, TokenURL: srvURL + "/token"}
	return func(out string, opts Options, logger *log.Logger) error {
		opts.Out = out
		_, err := RunGraph(context.Background(), g, opts, logger)
		return err
	}
}

// ma1337OwingCollapse is an archive an older MailArchive made of mailbox u1 (manifest version 3),
// holding one message filed twice, so it still owes the one-time v3 to v5 identity collapse.
func ma1337OwingCollapse(t *testing.T) string {
	t.Helper()
	out := tmpDir(t)
	srcID := state.MailboxSourceID("u1")
	seed, _ := state.Load(filepath.Join(out, "seed.json"))
	tok := seed.Token(srcID, "u1")
	entries := map[string]state.Record{}
	for _, folder := range []string{"Inbox", "Archive"} {
		rel := filepath.ToSlash(filepath.Join(tok, folder, "m1.html"))
		p := filepath.Join(out, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("<html>m1</html>"), 0o644); err != nil {
			t.Fatal(err)
		}
		entries[state.Key(tok, folder, "mid:m1@x")] = state.Record{Path: rel, Folder: folder, Fingerprint: "aaaaaaaaaaaaaaaa"}
	}
	raw, err := json.Marshal(struct {
		Version int                     `json:"version"`
		Stores  map[string]string       `json:"stores"`
		Entries map[string]state.Record `json:"entries"`
	}{Version: 3, Stores: map[string]string{srcID: tok}, Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return out
}

// covers: MA-1337, R5
// J1 and J3 for a Microsoft 365 capture: the folder comes to hold another archive (1) after the
// manifest's load, before the capture writes its history header or opens its index; (2) while it
// makes the one-time v3 to v5 identity collapse of an old archive, before that collapse's manifest
// save; (3) in its tail, after its last manifest save, before its folder pages, README and report.
// Each time the capture proves first, stops as a lost lock in the lock's words, and writes nothing
// at all into the other archive. Positive twins first: the same captures without the swap end ok,
// and the old archive's collapse is made and logged.
func TestACaptureWhoseFolderIsSwappedOutsideItsWalkWritesNothingIntoTheOther(t *testing.T) {
	_, srv := newFakeGraphServer()
	defer srv.Close()
	capture := ma1337Capture(t, srv.URL)
	quiet := discard()
	full := Options{Mode: export.Full, Index: true, Pages: true}
	twin := filepath.Join(tmpDir(t), "archive")
	if err := capture(twin, full, quiet); err != nil {
		t.Fatal(err)
	}
	if err := capture(twin, full, quiet); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	var seen strings.Builder
	var mu sync.Mutex
	collapsed := &ma1337Logger{marker: "collapsed", at: func() { mu.Lock(); seen.WriteString("collapsed"); mu.Unlock() }}
	if err := capture(ma1337OwingCollapse(t), Options{Mode: export.Incremental}, log.New(collapsed, "", 0)); err != nil || seen.String() != "collapsed" {
		t.Fatalf("positive twin: the old archive's capture: %v (collapse logged: %q)", err, seen.String())
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open")
	}
	for _, c := range []struct {
		name string
		outA func(t *testing.T) string
		opts Options
		at   string // a log line to swap at; "walk-done" swaps after the last mailbox; "" at the tail
	}{
		{"after-the-manifest-load", func(t *testing.T) string {
			out := filepath.Join(tmpDir(t), "archive")
			if err := capture(out, full, quiet); err != nil {
				t.Fatal(err)
			}
			stale := filepath.Join(out, ".mailarchive-stale.tmp") // a crashed run's temp: the sweep logs it
			if err := os.WriteFile(stale, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-time.Hour)
			if err := os.Chtimes(stale, old, old); err != nil {
				t.Fatal(err)
			}
			return out
		}, full, "orphaned temp"},
		{"at-the-identity-collapse", ma1337OwingCollapse, Options{Mode: export.Incremental}, "collapsed"},
		{"after-the-walk", func(t *testing.T) string {
			out := filepath.Join(tmpDir(t), "archive")
			if err := capture(out, full, quiet); err != nil {
				t.Fatal(err)
			}
			return out
		}, full, "walk-done"},
		{"in-the-tail", func(t *testing.T) string {
			out := filepath.Join(tmpDir(t), "archive")
			if err := capture(out, full, quiet); err != nil {
				t.Fatal(err)
			}
			return out
		}, full, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			outA := c.outA(t)
			outB := filepath.Join(tmpDir(t), "archive")
			if err := capture(outB, full, quiet); err != nil {
				t.Fatal(err)
			}
			before := ma1336Files(t, outB)
			swapped := false
			swap := func() {
				if !swapped {
					swapped = true
					ma1328Swap(t, outA, outA+"-drive1", outB)
				}
			}
			logger := quiet
			switch c.at {
			case "":
				tailForTest = swap
				defer func() { tailForTest = nil }()
			case "walk-done":
				// Every mailbox walked and saved: the footer and the last save come next.
				afterWalkForTest = swap
				defer func() { afterWalkForTest = nil }()
			default:
				logger = log.New(&ma1337Logger{marker: c.at, at: swap}, "", 0)
			}
			err := capture(outA, c.opts, logger)
			if !swapped {
				t.Fatal("premise: the swap was not reached")
			}
			rc, msg := ma1328LockLost(err)
			assure.Refused(t, rc, msg, assure.Names(filepath.Join(outA, lockfile.Name), "during the run", "run again"),
				assure.NoSideEffect(func() bool { return ma1336Untouched(t, outA, before) }))
		})
	}
}
