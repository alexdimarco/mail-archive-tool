package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/server"
	"mail-archive-tool/internal/state"
)

// The P3-4 adversarial pass's fixes (docs/review-encrypted-archives-phase3-p34-adversarial.md).

// realKnownArchives is knownArchives as the program has it; TestMain stubs it out.
var realKnownArchives = knownArchives

// covers: MA-828, R37
// The copy is made only in the folder its plan checked (DEST-01, FID-02): a parent
// replaced by another folder between the plan and the start is refused as stale, with
// nothing made in either; a parent swapped for a link once the copy holds it never
// sends the copy through the link — the copy stops, marked unfinished, and the place
// the link points to receives nothing. The positive twin is every whole copy.
func TestTheCopyIsMadeOnlyInTheFolderItsPlanChecked(t *testing.T) {
	f := newFakeCopy(t, copyMsg("a"))
	e := planEnv()
	base := tmpDir(t)
	parent := filepath.Join(base, "copies")
	os.Mkdir(parent, 0o700)
	p, err := e.PlanDecryptCopy(f.out, parent, nil, nil, planDay)
	if err != nil {
		t.Fatal(err)
	}
	// Replaced before the start: the same path, another folder.
	os.Rename(parent, parent+"-was")
	os.Mkdir(parent, 0o700)
	_, err = e.DecryptSealed(context.Background(), p, nil, nil, f, nil, nil)
	assure.Refused(t, 1, fmt.Sprint(err), assure.Code(1), assure.Names(parent, "not the folder it was"),
		assure.NoSideEffect(func() bool {
			a, _ := os.ReadDir(parent)
			b, _ := os.ReadDir(parent + "-was")
			return errors.Is(err, ErrDecryptStale) && len(a) == 0 && len(b) == 0
		}))

	// Swapped for a link once held: the copy is not written through it.
	f = newFakeCopy(t, copyMsg("a"), copyMsg("b"))
	parent2 := filepath.Join(base, "copies2")
	os.Mkdir(parent2, 0o700)
	elsewhere := filepath.Join(base, "elsewhere")
	os.Mkdir(elsewhere, 0o700)
	p, err = e.PlanDecryptCopy(f.out, parent2, nil, nil, planDay)
	if err != nil {
		t.Fatal(err)
	}
	f.onHist = func() {
		os.Rename(parent2, parent2+"-held")
		os.Symlink(elsewhere, parent2)
		os.Mkdir(filepath.Join(elsewhere, p.Name), 0o700) // a folder of the copy's name waits there
	}
	rep, err := e.DecryptSealed(context.Background(), p, nil, nil, f, nil, nil)
	held := filepath.Join(parent2+"-held", p.Name)
	assure.Refused(t, 1, fmt.Sprint(err), assure.Code(1), assure.Names("moved or replaced"),
		assure.NoSideEffect(func() bool {
			got, _ := os.ReadDir(filepath.Join(elsewhere, p.Name))
			_, merr := os.Stat(filepath.Join(held, CopyMarkerName))
			_, nerr := os.Stat(filepath.Join(held, plainManifestName))
			return errors.Is(err, ErrCopyFailed) && rep.Unfinished && len(got) == 0 && merr == nil && errors.Is(nerr, fs.ErrNotExist)
		}))
}

// covers: MA-828, R37
// "May leave this computer" (K5) is judged where links lead too (DEST-02, LEAK-01):
// a link to a synced folder, a link onto a removable drive, and — on macOS — a link
// to the Desktop or Documents folder each ask twice, as the folders themselves do.
// And it is judged again at the start (LEAK-02): a reason the plan did not have makes
// the plan stale, with nothing made. A plain local folder asks once (the positive
// twin, TestTheCopysPlanNamesItsFolderAndWritesNothing).
func TestTheCopyAsksTwiceThroughALinkAndAgainAtTheStart(t *testing.T) {
	out := sealedArchive(t)
	t.Setenv("OneDrive", "")
	t.Setenv("OneDriveCommercial", "")
	t.Setenv("OneDriveConsumer", "")
	base := tmpDir(t)
	home := filepath.Join(base, "home")
	os.MkdirAll(filepath.Join(home, "Documents", "mail"), 0o700)
	t.Setenv("HOME", home)
	synced := filepath.Join(base, "Dropbox", "notes")
	os.MkdirAll(synced, 0o700)
	stick := filepath.Join(base, "stick")
	os.Mkdir(stick, 0o700)
	link := func(name, to string) string {
		l := filepath.Join(base, name)
		if err := os.Symlink(to, l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	cases := []struct {
		name, parent, says string
		removable          func(string) bool
	}{
		{"a link to a synced folder", link("plain-name", synced), "synced to Dropbox", nil},
		{"a link onto a removable drive", link("to-stick", stick), "removable or external drive", func(d string) bool { return d == resolved(stick) }},
	}
	if runtime.GOOS == "darwin" {
		cases = append(cases, struct {
			name, parent, says string
			removable          func(string) bool
		}{"a link to Documents", link("docs", filepath.Join(home, "Documents", "mail")), "may be synced to iCloud", nil})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := planEnv()
			if c.removable != nil {
				e.removableSeam = c.removable
			}
			p, err := e.PlanDecryptCopy(out, c.parent, nil, nil, planDay)
			if err != nil {
				t.Fatal(err)
			}
			assure.Reached(t, p.AskTwice() && len(p.Leaves) == 1 && strings.Contains(p.Leaves[0], c.says), "asks twice: "+strings.Join(p.Leaves, "; "))
		})
	}

	f := newFakeCopy(t, copyMsg("a"))
	e := planEnv()
	parent := filepath.Join(base, "local")
	os.Mkdir(parent, 0o700)
	p, err := e.PlanDecryptCopy(f.out, parent, nil, nil, planDay)
	if err != nil || p.AskTwice() {
		t.Fatalf("positive twin: a local folder asks once: %v %v", p.Leaves, err)
	}
	e.removableSeam = func(string) bool { return true } // a drive mounted over it since the plan
	_, err = e.DecryptSealed(context.Background(), p, nil, nil, f, nil, nil)
	assure.Refused(t, 1, fmt.Sprint(err), assure.Code(1), assure.Names("removable", "plan the copy again"),
		assure.NoSideEffect(func() bool {
			got, _ := os.ReadDir(parent)
			return errors.Is(err, ErrDecryptStale) && len(got) == 0
		}))
}

func resolved(p string) string {
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return r
}

// covers: MA-828, R37
// The copy is made outside every archive this computer knows, from either front end
// (DEST-05): the Desktop's list, its current archive and the wizard's last one are read
// from MailArchive's settings folder; a folder inside one is refused (R-f) with nothing
// written. The positive twin: with none known, the same folder plans.
func TestTheCopyStaysOutsideEveryArchiveThisComputerKnows(t *testing.T) {
	out := sealedArchive(t)
	known := tmpDir(t)
	inside := filepath.Join(known, "sub")
	os.Mkdir(inside, 0o700)
	// An alias of it that a path check cannot see: a link (as a subst letter or a bind
	// mount would be).
	alias := filepath.Join(tmpDir(t), "alias")
	os.Symlink(inside, alias)
	e := planEnv()
	if _, err := e.PlanDecryptCopy(out, alias, nil, nil, planDay); err != nil {
		t.Fatalf("positive twin: with no archive known the folder plans: %v", err)
	}

	home := tmpDir(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	d, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(d, 0o700)
	desk, last := filepath.Join(tmpDir(t), "desk"), filepath.Join(tmpDir(t), "last")
	if err := archindex.WriteFile(filepath.Join(d, archindex.Name), archindex.Index{Version: archindex.Version, Entries: []archindex.Entry{{Path: known}}}); err != nil {
		t.Fatal(err)
	}
	sf, _ := job.DesktopSettingsFile()
	os.WriteFile(sf, []byte(`{"out":"`+filepath.ToSlash(desk)+`"}`), 0o600)
	lf, _ := job.LastArchiveFile()
	os.WriteFile(lf, []byte(last+"\n"), 0o600)
	got := realKnownArchives()
	want := []string{known, desk, last}
	assure.Reached(t, strings.Join(got, "|") == strings.Join(want, "|"), "the known archives read: "+strings.Join(got, "|"))

	knownArchives = func() []string { return []string{known} }
	defer func() { knownArchives = func() []string { return nil } }()
	before := snapshotTree(t, inside)
	_, err = e.PlanDecryptCopy(out, alias, nil, nil, planDay)
	var r *CopyRefusal
	assure.Refused(t, 1, fmt.Sprint(err), assure.Code(1), assure.Names("archive"),
		assure.NoSideEffect(func() bool { return errors.As(err, &r) && r.ID == "R-f" && sameTree(before, snapshotTree(t, inside)) }))
}

// covers: MA-829, R37
// The copy's search index never misleads (CRIT-01, FAIL-06): a sealed index marked
// behind — missing messages the archive holds — and one that would list messages the
// copy left out are not kept as they are; the copy's index is rebuilt from the files
// it holds, one row for each message copied. The positive twin keeps the image.
func TestTheCopysIndexHoldsExactlyWhatItCopied(t *testing.T) {
	f := newFakeCopy(t, copyMsg("a"), copyMsg("b"))
	rep, _, err := copyOf(t, f, nil, nil)
	if err != nil || rep.Index != "copied" {
		t.Fatalf("positive twin: a whole copy keeps the image: %+v %v", rep, err)
	}
	for _, c := range []struct {
		name  string
		setup func(f *fakeCopy)
	}{
		{"marked behind", func(f *fakeCopy) { f.behind = true }},
		{"a message left out", func(f *fakeCopy) { f.fail["s/Inbox/b.html"] = fs.ErrNotExist }},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeCopy(t, copyMsg("a"), copyMsg("b"))
			c.setup(f)
			rep, p, err := copyOf(t, f, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			ix, err := index.Open(filepath.Join(p.Dest, "search.db"))
			if err != nil {
				t.Fatal(err)
			}
			n, _ := ix.Count()
			ix.Close()
			assure.Reached(t, rep.Index == "rebuilt" && n == rep.Copied && n > 0, fmt.Sprintf("rebuilt, %d rows for %d copied", n, rep.Copied))
		})
	}
}

// covers: MA-829, R37
// A copy that cannot be shown to be the planned archive is not reported whole: on a
// read-only medium the record is compared with the plan's, so a change made while the
// record was read is seen (FAIL-02, FID-10); and the copy so failed — like one with a
// link in it — holds no manifest, so nothing takes it for an archive (FAIL-05).
func TestALockFreeCopyIsComparedWithItsPlan(t *testing.T) {
	orig := copyLock
	copyLock = func(path string) (*lockfile.Lock, error) {
		return nil, &fs.PathError{Op: "open", Path: path, Err: syscall.EROFS}
	}
	defer func() { copyLock = orig }()
	f := newFakeCopy(t, copyMsg("a"), copyMsg("b"))
	mirror := filepath.Join(f.out, container.MirrorName)
	f.onLoad = func() {
		b, _ := os.ReadFile(mirror)
		os.WriteFile(mirror, append(b, ' '), 0o600)
	}
	rep, p, err := copyOf(t, f, nil, nil)
	_, merr := os.Stat(filepath.Join(p.Dest, plainManifestName))
	assure.Refused(t, 1, fmt.Sprint(err), assure.Code(1), assure.Names("changed while", p.Dest),
		assure.NoSideEffect(func() bool { return errors.Is(err, ErrCopyFailed) && rep.Unfinished && errors.Is(merr, fs.ErrNotExist) }))

	copyLock = orig
	f = newFakeCopy(t, copyMsg("a"), copyMsg("b"))
	f.during = func(rel string) {
		if rel == "s/Inbox/b.html" {
			os.Symlink(os.TempDir(), filepath.Join(f.dest, "s", "Inbox", "link"))
		}
	}
	rep, p, err = copyOf(t, f, nil, nil)
	_, merr = os.Stat(filepath.Join(p.Dest, plainManifestName))
	assure.Reached(t, errors.Is(err, ErrCopyFailed) && rep.Unfinished && errors.Is(merr, fs.ErrNotExist), "a copy failed at its last walk holds no manifest")
}

// covers: MA-829, R37
// A stop is heard to the end (FAIL-03): after the last message — while the history,
// the index or the pages are made — it stops the copy as ErrCopyStopped, marked
// unfinished, never reported whole.
func TestAStopAfterTheLastMessageStopsTheCopy(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(f *fakeCopy, cancel func())
	}{
		{"during the history", func(f *fakeCopy, cancel func()) { f.onHist = cancel }},
		{"during the index", func(f *fakeCopy, cancel func()) { f.onImage = cancel }},
		{"before a rebuild", func(f *fakeCopy, cancel func()) { f.behind = true; f.onHist = cancel }},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeCopy(t, copyMsg("a"), copyMsg("b"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.setup(f, cancel)
			rep, p, err := copyOf(t, f, ctx, nil)
			_, merr := os.Stat(filepath.Join(p.Dest, CopyMarkerName))
			assure.Reached(t, errors.Is(err, ErrCopyStopped) && rep.Unfinished && merr == nil, fmt.Sprintf("stopped: %v", err))
		})
	}
	// The last moment: a stop just before the mark comes off keeps it (CRIT-06); the
	// copy is never reported whole.
	f0 := newFakeCopy(t, copyMsg("a"))
	e0 := planEnv()
	p0, err0 := e0.PlanDecryptCopy(f0.out, tmpDir(t), nil, nil, planDay)
	if err0 != nil {
		t.Fatal(err0)
	}
	ctx0, cancel0 := context.WithCancel(context.Background())
	defer cancel0()
	e0.faults = &copyFaults{hook: func(step string) {
		if step == "decrypt-unmark" {
			cancel0()
		}
	}}
	rep0, err0 := e0.DecryptSealed(ctx0, p0, nil, nil, f0, nil, nil)
	_, m0 := os.Stat(filepath.Join(p0.Dest, CopyMarkerName))
	_, n0 := os.Stat(filepath.Join(p0.Dest, plainManifestName))
	assure.Reached(t, errors.Is(err0, ErrCopyStopped) && rep0.Unfinished && m0 == nil && errors.Is(n0, fs.ErrNotExist), fmt.Sprintf("a stop at the last moment: %v", err0))

	// The rebuild itself hears it between messages.
	f := newFakeCopy(t, copyMsg("a"))
	_, p, err := copyOf(t, f, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(filepath.Join(tmpDir(t), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m, _ := state.Load(filepath.Join(p.Dest, plainManifestName))
	n, _ := ix.Count()
	assure.Reached(t, errors.Is(indexCopied(ctx, p.Dest, m, ix), context.Canceled) && n == 0, "a rebuild stopped adds nothing")
}

// covers: MA-829, R37
// Names the copy keeps for itself are kept in any letter case (FAIL-04): a record
// named like a folder page, the README or the search index in another case is left
// out and listed — never written over, nor written over by the copy's own — and the
// copy's README is its own file.
func TestReservedNamesAreKeptInAnyCase(t *testing.T) {
	good := copyMsg("good")
	page := fakeMsg{path: "s/Inbox/Index.html", files: map[string][]byte{".html": []byte("<html>page</html>")}}
	readme := fakeMsg{path: "Readme.TXT/m.html", files: map[string][]byte{".html": []byte("x")}}
	db := fakeMsg{path: "Search.DB/m.html", files: map[string][]byte{".html": []byte("x")}}
	f := newFakeCopy(t, good, page, readme, db)
	rep, p, err := copyOf(t, f, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	problems, _ := os.ReadFile(filepath.Join(p.Dest, CopyProblemsName))
	fi, rerr := os.Lstat(filepath.Join(p.Dest, "README.txt"))
	ok := rep.Skipped == 3 && rep.Copied == 1 && rerr == nil && fi.Mode().IsRegular()
	for _, rel := range []string{"s/Inbox/Index.html", "Readme.TXT/m.html", "Search.DB/m.html"} {
		ok = ok && strings.Contains(string(problems), rel+": its name is one the copy keeps")
	}
	assure.Reached(t, ok, fmt.Sprintf("%+v\n%s", rep, problems))
}

// covers: MA-829, R37
// The copy carries a lost part of the timeline (FID-05): when the archive's record
// marks part of its timeline lost, the copy's history says so, and the copy's own
// go-back view — the plaintext reader's — is partial, as the archive's is. The
// positive twin: a whole timeline is available in the copy.
func TestTheCopysGoBackViewIsPartialWhenTheArchivesIs(t *testing.T) {
	for _, gap := range []bool{false, true} {
		f := newFakeCopy(t, copyMsg("a"))
		f.gap = gap
		rep, p, err := copyOf(t, f, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		g, err := server.Folder(p.Dest, nil).GoBack()
		want := server.TimelineAvailable
		if gap {
			want = server.TimelinePartial
		}
		assure.Reached(t, err == nil && g.Timeline == want && rep.HistoryGap == gap, fmt.Sprintf("gap %v: timeline %v", gap, g.Timeline))
	}
}

// covers: MA-829, R37
// The copy's report and history are the archive's (FID-06): a record with an issue is
// in the copy's attachments-report.tsv exactly as the plaintext report of the same
// record says it, and the copy's history reads back as the events served.
func TestTheCopysReportAndHistoryAreTheArchives(t *testing.T) {
	odd := copyMsg("odd")
	odd.missing = []string{"attachment:1"}
	odd.fallback = "text"
	f := newFakeCopy(t, copyMsg("a"), odd)
	_, p, err := copyOf(t, f, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := tmpDir(t)
	writeReport(want, f.m, false, silentLogger())
	a, aerr := os.ReadFile(filepath.Join(p.Dest, reportName))
	b, berr := os.ReadFile(filepath.Join(want, reportName))
	got, herr := state.ReadHistory(filepath.Join(p.Dest, state.HistoryName))
	same := reflect.DeepEqual(got, f.history)
	assure.Reached(t, aerr == nil && berr == nil && len(a) > 0 && bytes.Equal(a, b) && strings.Contains(string(a), "odd") && herr == nil && same,
		"the copy's report and history match the archive's")
}

// covers: MA-820, R37
// The copy holds the archive's lock while it copies (FID-03): a backup's lock taken
// mid-copy is refused; the copy's files are 0644 and its folders 0755 inside the
// 0700 copy folder (FID-09).
func TestTheCopyHoldsTheLockAndItsFilesModes(t *testing.T) {
	f := newFakeCopy(t, copyMsg("a"), copyMsg("b"))
	var held error
	f.during = func(rel string) {
		if rel == "s/Inbox/b.html" {
			l, err := lockfile.AcquireAs(filepath.Join(f.out, lockfile.Name), "backup")
			if err == nil {
				l.Release()
			}
			held = err
		}
	}
	_, p, err := copyOf(t, f, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(filepath.Join(p.Dest, "s", "Inbox", "a.eml"))
	di, _ := os.Stat(filepath.Join(p.Dest, "s", "Inbox"))
	modes := runtime.GOOS == "windows" || (fi.Mode().Perm() == 0o644 && di.Mode().Perm() == 0o755)
	assure.Reached(t, errors.Is(held, lockfile.ErrHeld) && modes, fmt.Sprintf("lock mid-copy: %v; modes %v %v", held, fi.Mode().Perm(), di.Mode().Perm()))
}

// covers: MA-820, R37
// A scheduled Microsoft 365 backup refuses an unfinished copy too (FID-09), as Run
// does: no new mail is mixed into it.
func TestGraphRefusesAnUnfinishedCopy(t *testing.T) {
	f := newFakeCopy(t, copyMsg("a"), copyMsg("b"))
	f.fail["s/Inbox/b.html"] = fmt.Errorf("%w: the key", ErrCopyLocked)
	_, p, err := copyOf(t, f, nil, nil)
	if !errors.Is(err, ErrCopyLocked) {
		t.Fatal(err)
	}
	before := snapshotTree(t, p.Dest)
	_, err = RunGraph(context.Background(), GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"}}, Options{Out: p.Dest}, discard())
	assure.Refused(t, 1, fmt.Sprint(err), assure.Code(1), assure.Names(p.Dest, "did not finish"),
		assure.NoSideEffect(func() bool { return sameTree(before, snapshotTree(t, p.Dest)) }))
}

// covers: MA-829, R37
// The copy's index keeps SQLite's temporary work in memory when it is small
// (LEAK-05): its sorts never spill subjects and senders to the system's temporary
// folder; a larger one is opened as any index (the spill stays in §14's budget).
func TestTheCopysIndexKeepsItsWorkInMemory(t *testing.T) {
	dir := tmpDir(t)
	small, err := openCopyIndex(filepath.Join(dir, "small.db"), true)
	if err != nil {
		t.Fatal(err)
	}
	defer small.Close()
	large, err := openCopyIndex(filepath.Join(dir, "large.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer large.Close()
	assure.Reached(t, small.TempInMemory() && !large.TempInMemory(), "small in memory, large as any index")
}
