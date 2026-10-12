package app

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// The cases of the rows FI-S2b lands (design-fault-isolation §4.5, §9): the local
// readers' per-file and per-folder isolation and their containment under R25.

// maildirStore writes folders of maildir files, each given as "Folder/cur/name": the
// message in every file is named by the part of the name before its first ':'.
func maildirStore(t *testing.T, store string, files ...string) {
	t.Helper()
	for _, f := range files {
		name := filepath.Base(f)
		id := strings.SplitN(name, ":", 2)[0]
		p14Write(t, filepath.Join(store, filepath.FromSlash(f)), strings.ReplaceAll(p14Plain("subject "+id, id), "\r\n", "\n"))
	}
}

// recordCount is how many records the archive's manifest holds.
func recordCount(t *testing.T, out string) int { return loadArchive(t, out).Len() }

func gapReasons(gaps []state.CaptureGap) string {
	var r []string
	for _, g := range gaps {
		r = append(r, g.Reason+"@"+g.Locator)
	}
	return strings.Join(r, " ")
}

// covers: MA-321, R10, R15, R1, S46
// Maildir per-file isolation end to end (F29a, FC7): Inbox/cur{a, b chmod 000, c} and
// Work/cur{d} archive a, c and d; b is ONE file-unreadable fillable ledger gap at
// maildir:Inbox/<unique>, with NO record, the run is partial and says "fix the
// permissions". After chmod 600 the next run archives b and its gap closes as
// recovered. The twin first: the same store with no fault archives all four and
// records nothing.
func TestMaildirFileFaultsCostOneFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a unix permission check that root and Windows do not make")
	}
	store := filepath.Join(tmpDir(t), "mail")
	maildirStore(t, store, "Inbox/cur/a:2,S", "Inbox/cur/b:2,S", "Inbox/cur/c:2,S", "Work/cur/d:2,S")

	twinOut := tmpDir(t)
	if res, err := runLocal(t, twinOut, store); err != nil || res.Partial || recordCount(t, twinOut) != 4 || len(ledgerOf(t, twinOut)) != 0 {
		t.Fatalf("twin: err %v, partial %v, %d records, ledger %v; want 4 records and no gap", err, res.Partial, recordCount(t, twinOut), ledgerOf(t, twinOut))
	}

	bad := filepath.Join(store, "Inbox", "cur", "b:2,S")
	if err := os.Chmod(bad, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(bad, 0o644) })
	out := tmpDir(t)
	res, err := runLocal(t, out, store)
	if !errors.Is(err, ErrPartial) || !res.Partial {
		t.Fatalf("run 1 err = %v; want ErrPartial", err)
	}
	if n := recordCount(t, out); n != 3 {
		t.Errorf("%d records; want a, c and d only: the unreadable file mints no record (FC7)", n)
	}
	gaps := assure.Reached(t, ledgerOf(t, out), "the ledger")
	g := gaps[0]
	if len(gaps) != 1 || g.Reason != "file-unreadable" || g.Class != state.ClassFillable || g.Scope != state.GapScopeItem || g.Locator != "maildir:Inbox/b" || g.Folder != "Inbox" {
		t.Fatalf("ledger %s; want one fillable item gap file-unreadable at maildir:Inbox/b", gapReasons(gaps))
	}
	if lines := strings.Join(res.Gaps.Lines, "\n"); !strings.Contains(lines, "fix the permissions") {
		t.Errorf("summary %q; want the permission remedy", lines)
	}

	// The remedy followed: chmod 600.
	if err := os.Chmod(bad, 0o600); err != nil {
		t.Fatal(err)
	}
	ageLedger(t, out, 48*time.Hour)
	res, err = runLocal(t, out, store)
	if err != nil || res.Partial {
		t.Fatalf("run 2 err %v, partial %v; want clean", err, res.Partial)
	}
	if recordCount(t, out) != 4 || len(ledgerOf(t, out)) != 0 || res.Gaps.Recovered != 1 {
		t.Errorf("%d records, ledger %s, recovered %d; want b archived and its gap closed as recovered", recordCount(t, out), gapReasons(ledgerOf(t, out)), res.Gaps.Recovered)
	}
}

// covers: MA-321, R10, R1, S46
// A live mail client changes a maildir file between the listing and the read: a flag
// rename (b:2, to b:2,S, during the export of a) is found once by its unique part and
// read, no gap; a deletion (during the export of a) is a fillable vanished-during-read
// gap that makes the run partial (owner ruling OR4: a first sighting counts) and closes
// by itself on the next run, which no longer finds the file.
func TestMaildirFilesChangedUnderTheRunAreFoundOrRecorded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("maildir names carry a colon")
	}
	for _, c := range []struct {
		name   string
		change func(path string) error
	}{
		{"renamed", func(p string) error { return os.Rename(p, p+"S") }},
		{"deleted", os.Remove},
	} {
		t.Run(c.name, func(t *testing.T) {
			store := filepath.Join(tmpDir(t), "mail")
			maildirStore(t, store, "Inbox/cur/a:2,S", "Inbox/cur/b:2,", "Inbox/cur/c:2,S")
			victim := filepath.Join(store, "Inbox", "cur", "b:2,")
			fired := false
			exporterSetup = func(e *export.Exporter) {
				e.BeforeWrite = func(path string) error {
					if !fired && strings.HasSuffix(path, ".html") {
						fired = true
						return c.change(victim)
					}
					return nil
				}
			}
			t.Cleanup(func() { exporterSetup = nil })
			out := tmpDir(t)
			res, err := runLocal(t, out, store)
			if !fired {
				t.Fatal("the hook never ran")
			}
			if c.name == "renamed" {
				if err != nil || res.Partial || recordCount(t, out) != 3 || len(ledgerOf(t, out)) != 0 {
					t.Errorf("renamed: err %v, partial %v, %d records, ledger %s; want b found under its new name: 3 records, no gap", err, res.Partial, recordCount(t, out), gapReasons(ledgerOf(t, out)))
				}
				return
			}
			if !errors.Is(err, ErrPartial) || recordCount(t, out) != 2 {
				t.Fatalf("deleted: err %v, %d records; want ErrPartial and a and c", err, recordCount(t, out))
			}
			g := ledgerOf(t, out)
			if len(g) != 1 || g[0].Reason != "vanished-during-read" || g[0].Class != state.ClassFillable || g[0].Locator != "maildir:Inbox/b" {
				t.Fatalf("ledger %s; want one fillable vanished-during-read at maildir:Inbox/b", gapReasons(g))
			}
			exporterSetup = nil
			ageLedger(t, out, 48*time.Hour)
			res, err = runLocal(t, out, store)
			if err != nil || res.Partial || len(ledgerOf(t, out)) != 0 || res.Gaps.ClosedGone != 1 {
				t.Errorf("run 2: err %v, partial %v, ledger %s, closed %d; want the self-closing gap closed", err, res.Partial, gapReasons(ledgerOf(t, out)), res.Gaps.ClosedGone)
			}
		})
	}
}

// pageAndIndexBytes is every byte the archive holds that a reader could be served:
// each exported page and the search index.
func pageAndIndexBytes(t *testing.T, out string) []byte {
	t.Helper()
	var all bytes.Buffer
	err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasSuffix(p, ".html") || strings.HasSuffix(p, ".eml") || strings.HasSuffix(p, ".db") || strings.HasSuffix(p, "-wal") {
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			all.Write(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return all.Bytes()
}

// covers: MA-322, R25, R10, R4, S47
// Containment end to end (F29b/c/d). A maildir store holds a FIFO, a link to a file
// OUTSIDE the store (a secret), a link to a file inside it, an absolute link, and a
// linked cur/: the run completes under a guard, the secret is ABSENT from every page and
// from search.db, the in-store link is read, and each refusal is a ledger gap of the
// right class (the FIFO info, the links terminal, the run partial). The positive twin
// first: the same store without them archives everything with no gap.
func TestLocalStoresAreReadUnderContainment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links and FIFOs need privileges on Windows")
	}
	parent := tmpDir(t)
	store := filepath.Join(parent, "mail")
	maildirStore(t, store, "Inbox/cur/a:2,S", "Inbox/cur/real:2,S")
	twinOut := tmpDir(t)
	if res, err := runLocalIndexed(t, twinOut, store); err != nil || res.Partial || recordCount(t, twinOut) != 2 || len(ledgerOf(t, twinOut)) != 0 {
		t.Fatalf("twin: err %v, partial %v, %d records, ledger %v; want 2 records and no gap", err, res.Partial, recordCount(t, twinOut), ledgerOf(t, twinOut))
	}

	const marker = "TOP-SECRET-CONTENT SSN 123-45-6789"
	secret := filepath.Join(parent, "outside", "secret")
	p14Write(t, secret, strings.ReplaceAll(p14Plain(marker, "secret"), "\r\n", "\n"))
	p14Write(t, filepath.Join(parent, "outsidedir", "cur", "m"), strings.ReplaceAll(p14Plain(marker+" dir", "outdir"), "\r\n", "\n"))
	cur := filepath.Join(store, "Inbox", "cur")
	if err := mkfifo(filepath.Join(cur, "pipe")); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	for target, name := range map[string]string{
		secret:                    filepath.Join(cur, "abs"),
		"../../../outside/secret": filepath.Join(cur, "dots"),
		"real:2,S":                filepath.Join(cur, "inlink"),
	} {
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	maildirStore(t, store, "Other/new/n:2,")
	if err := os.Symlink(filepath.Join(parent, "outsidedir", "cur"), filepath.Join(store, "Other", "cur")); err != nil {
		t.Fatal(err)
	}

	out := tmpDir(t)
	done := make(chan struct{})
	var res Result
	var err error
	go func() {
		defer close(done)
		res, err = runLocalIndexed(t, out, store)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not finish: a pipe or a link was waited on")
	}
	if !errors.Is(err, ErrPartial) || !res.Partial {
		t.Fatalf("run err = %v; want ErrPartial: refused links that might hold mail are terminal gaps", err)
	}
	if all := pageAndIndexBytes(t, out); bytes.Contains(all, []byte("TOP-SECRET-CONTENT")) || bytes.Contains(all, []byte("123-45-6789")) {
		t.Error("the content of a file outside the store reached a page or the search index (NoSideEffect)")
	}
	gaps := ledgerOf(t, out)
	count := map[string]int{}
	for _, g := range gaps {
		count[g.Reason+"/"+g.Class]++
	}
	want := map[string]int{"not-a-message-file/info": 1, "symlink-outside-store/terminal": 2, "symlinked-folder-skipped/terminal": 1}
	for k, n := range want {
		if count[k] != n {
			t.Errorf("ledger %s; want %d of %s", gapReasons(gaps), n, k)
		}
	}
	// The in-store link reads its target under its own name: a, real, inlink, and Other's n.
	if n := recordCount(t, out); n != 3 {
		// "inlink" is the same message as "real" (same Message-ID, same folder): one record.
		t.Errorf("%d records; want a, real and n", n)
	}
	for _, g := range gaps {
		if g.Reason == "symlinked-folder-skipped" {
			remedy := state.GapRemedy(g.Reason, state.SourceLocal, "")
			assure.Refused(t, 1, remedy, assure.Code(1), assure.Names("-follow-links", "keeps its archive"))
		}
	}

	// Discovery does not wait on a pipe either: a store whose entry is a FIFO named like
	// a mailbox is found within the guard.
	if err := mkfifo(filepath.Join(store, "Trap")); err != nil {
		t.Fatal(err)
	}
	found := make(chan []string, 1)
	go func() {
		got, _ := DiscoverInputs([]string{store}, false)
		found <- got
	}()
	select {
	case got := <-found:
		if len(got) != 1 {
			t.Errorf("discovery found %v; want the store as one source", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("discovery waited on a pipe")
	}
}

// runLocalIndexed is runLocal with the search index on, so a test can look for what
// reached it.
func runLocalIndexed(t *testing.T, out string, inputs ...string) (Result, error) {
	t.Helper()
	return Run(context.Background(), Options{Inputs: inputs, Out: out, Mode: export.Incremental, Index: true, Pages: true}, discard(), nil)
}

// covers: MA-322, R25, R10, R4, S47
// The mbox layout end to end: a FIFO named Inbox at the store root and an Inbox-like
// link to an outside file that begins "From " are refused and recorded, the secret is
// absent; a .sbd container linked to the store's root is not followed (each message is
// read once, not once per level: F29d) and is a terminal symlinked-folder-skipped gap.
func TestMboxLayoutIsReadUnderContainment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links and FIFOs need privileges on Windows")
	}
	parent := tmpDir(t)
	store := mboxStore(t, parent, "tb", []string{"Archive"}, 3)
	secret := filepath.Join(parent, "outside")
	p14Write(t, secret, "From x@example.com Mon Mar  3 09:00:00 2025\n"+strings.ReplaceAll(p14Plain("TOP-SECRET-CONTENT", "secret"), "\r\n", "\n")+"\n")
	if err := mkfifo(filepath.Join(store, "Inbox")); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	for target, name := range map[string]string{secret: filepath.Join(store, "Sent"), store: filepath.Join(store, "Archive.sbd")} {
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	out := tmpDir(t)
	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		_, err = runLocalIndexed(t, out, store)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not finish: a pipe or a link loop was waited on or followed")
	}
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("run err = %v; want ErrPartial", err)
	}
	if n := recordCount(t, out); n != 3 {
		t.Errorf("%d records; want the 3 messages of Archive, once (a followed .sbd loop reads them again)", n)
	}
	if bytes.Contains(pageAndIndexBytes(t, out), []byte("TOP-SECRET-CONTENT")) {
		t.Error("a file outside the store reached a page or the index")
	}
	reasons := gapReasons(ledgerOf(t, out))
	for _, w := range []string{"not-a-message-file@file:Inbox", "symlink-outside-store@file:Sent", "symlinked-folder-skipped@folder:Archive.sbd"} {
		if !strings.Contains(reasons, w) {
			t.Errorf("ledger %s; want %s", reasons, w)
		}
	}
}

// covers: MA-322, R25, R10, S47
// The input's directory-link opt-in (owner ruling OR6), end to end. Run 1 without it
// archives the store and records its folder links as terminal gaps. Run 2 with the input
// named in Options.FollowLinks: the input keeps its token (no second tree, nothing
// already archived is archived again), the linked folder is read, the link back to the
// store's root is read ONCE (an info folder-cycle row replaces the gap), and the file
// link out of the store is still refused with the secret absent.
func TestDirectoryLinkOptInKeepsTheTokenAndFollowsOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links need privileges on Windows")
	}
	parent := tmpDir(t)
	store := mboxStore(t, parent, "tb", []string{"Archive"}, 3)
	elsewhere := filepath.Join(parent, "elsewhere")
	p14Write(t, filepath.Join(elsewhere, "Projects"), "From a@example.com Mon Mar  3 09:00:00 2025\n"+strings.ReplaceAll(p14Plain("proj", "proj"), "\r\n", "\n")+"\n")
	secret := filepath.Join(parent, "outside")
	p14Write(t, secret, "From x@example.com Mon Mar  3 09:00:00 2025\n"+strings.ReplaceAll(p14Plain("TOP-SECRET-CONTENT", "secret"), "\r\n", "\n")+"\n")
	p14Write(t, filepath.Join(store, "Linked"), "From a@example.com Mon Mar  3 09:00:00 2025\n"+strings.ReplaceAll(p14Plain("linked", "linked"), "\r\n", "\n")+"\n")
	for target, name := range map[string]string{store: filepath.Join(store, "Archive.sbd"), elsewhere: filepath.Join(store, "Linked.sbd"), secret: filepath.Join(store, "Sent")} {
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	out := tmpDir(t)
	if _, err := runLocalIndexed(t, out, store); !errors.Is(err, ErrPartial) {
		t.Fatalf("run 1 err = %v; want ErrPartial (the links are recorded)", err)
	}
	m1 := loadArchive(t, out)
	tok := m1.Stores[state.PathSourceID(store)]
	assure.Reached(t, tok, "the store's token")
	first := m1.Len()
	if r := gapReasons(ledgerOf(t, out)); !strings.Contains(r, "symlinked-folder-skipped@folder:Archive.sbd") || !strings.Contains(r, "symlinked-folder-skipped@folder:Linked.sbd") {
		t.Fatalf("run 1 ledger %s; want both folder links recorded", r)
	}

	ageLedger(t, out, 48*time.Hour)
	res, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Mode: export.Incremental, Index: true, Pages: true, FollowLinks: []string{store}}, discard(), nil)
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("run 2 err = %v; want ErrPartial (the file link out of the store is still refused)", err)
	}
	m2 := loadArchive(t, out)
	if m2.Stores[state.PathSourceID(store)] != tok || len(m2.Stores) != 1 {
		t.Errorf("stores %v; want the input to keep its token %q and no second one", m2.Stores, tok)
	}
	var trees []string
	entries, _ := os.ReadDir(out)
	for _, e := range entries {
		if e.IsDir() {
			trees = append(trees, e.Name())
		}
	}
	if len(trees) != 1 || trees[0] != tok {
		t.Errorf("archive trees %v; want the one tree %q: the opt-in mints no second tree", trees, tok)
	}
	// Archive's 3 messages and Linked's 1 were archived by run 1 and not again; Projects,
	// which sits in Linked's linked container, is the only new message.
	if m2.Len() != first+1 || res.Stats.Exported != 1 {
		t.Errorf("%d records after run 2 (run 1: %d), exported %d; want only Projects new (Linked was read by run 1)", m2.Len(), first, res.Stats.Exported)
	}
	if bytes.Contains(pageAndIndexBytes(t, out), []byte("TOP-SECRET-CONTENT")) {
		t.Error("the opt-in followed a file link out of the store")
	}
	reasons := gapReasons(ledgerOf(t, out))
	if !strings.Contains(reasons, "symlink-outside-store@file:Sent") || !strings.Contains(reasons, "folder-cycle@folder:Archive.sbd") || strings.Contains(reasons, "symlinked-folder-skipped") {
		t.Errorf("run 2 ledger %s; want Sent still refused, the loop an info folder-cycle, no refused folder link", reasons)
	}
	for _, g := range ledgerOf(t, out) {
		if g.Reason == "folder-cycle" && g.Class != state.ClassInfo {
			t.Errorf("%+v: a folder read through its other path is info", g)
		}
	}
	// The job `status` offers to schedule for this archive keeps the opt-in, or the
	// schedule would stop reading the links.
	if job := strings.Join(canonicalJob(Options{Inputs: []string{store}, Out: out, FollowLinks: []string{store}}, "incremental"), " "); !strings.Contains(job, "-follow-links "+absOrSame(store)) {
		t.Errorf("the canonical job %q lost the opt-in", job)
	}
}
