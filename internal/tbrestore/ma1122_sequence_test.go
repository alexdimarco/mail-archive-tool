package tbrestore

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/unicode/norm"
)

// covers: MA-1122, R40, S68
// A crash (a power cut) at every step of the write (design §5.4 as folded): at each,
// the parent file is absent unless the crash came after its create; the made-list
// names every entry on disk the restore made, and nothing it did not try to make
// (foldcheck M1); and the next Start settles it so that Local Folders is as it was
// (the leftover removed, by the list), or — a crash right after the Mkdir, before the
// marker — the empty container is left and the next restore takes " (2)" (N1), or —
// after the commit — the finished folder stays and is recorded as ended. Then a new
// restore always runs to the end (N-g).
func TestRestoreCrashAtEveryStep(t *testing.T) {
	in, msgs := sampleFolders()
	ops := opsOf(t, in, msgs)
	if len(ops) < 20 {
		t.Fatalf("the clean run's steps: %v", ops)
	}
	commitAt := -1
	for i, op := range ops {
		if op == "create Restored — Work mail" {
			commitAt = i + 1
		}
	}
	if commitAt < 0 {
		t.Fatal("no commit step seen")
	}
	for k := 1; k <= len(ops); k++ {
		fx := newFixture(t, in, msgs)
		before := fx.local()
		id, crashed := fx.writeCrashing(k)
		if !crashed {
			t.Fatalf("step %d (%s) did not crash", k, ops[k-1])
		}
		container := filepath.Join(fx.lf.Dir, "Restored — Work mail.sbd")
		parent := filepath.Join(fx.lf.Dir, "Restored — Work mail")
		_, perr := os.Lstat(parent)
		if k <= commitAt && perr == nil {
			t.Fatalf("crash at step %d (%s): the parent file exists before its create", k, ops[k-1])
		}
		if _, err := os.Lstat(filepath.Join(container, MadeListName)); err == nil {
			l := listed(t, fx.root, "Restored — Work mail.sbd")
			for _, e := range madeOnDisk(t, container) {
				if !l[e] {
					t.Fatalf("crash at step %d (%s): %s on disk is not on the made-list", k, ops[k-1], e)
				}
			}
		} else if es := madeOnDisk(t, container); len(es) != 0 {
			t.Fatalf("crash at step %d: entries before the made-list: %v", k, es)
		}
		s := fx.settle(false)
		if len(s) != 1 || s[0].Restore.Started.ID != id {
			t.Fatalf("crash at step %d: one open restore settled, got %d", k, len(s))
		}
		switch {
		case k > commitAt:
			if s[0].Case != "2" || s[0].Action != EvEnded {
				t.Fatalf("crash at step %d after the commit: case %s %s", k, s[0].Case, s[0].Action)
			}
		case s[0].Case == "1" && fileExists(container):
			// A container with no marker that proves it (none yet, or one cut off before its
			// line was written) cannot be shown to be MailArchive's: left (N1). It holds at
			// most that marker file.
			es, _ := os.ReadDir(container)
			if len(es) > 1 || (len(es) == 1 && es[0].Name() != MarkerName) {
				t.Fatalf("crash at step %d: case 1 with a container holding more than a torn marker: %v", k, es)
			}
		default:
			if got := fx.local(); got != before {
				t.Fatalf("crash at step %d (%s): settled as %s/%s, Local Folders not as it was:\n%s\n---\n%s", k, ops[k-1], s[0].Case, s[0].Action, before, got)
			}
		}
		if len(Open(Restores(mustRead(t, fx.log)), ProfileKeyOf(fx.profile))) != 0 {
			t.Fatalf("crash at step %d: still open after Start", k)
		}
		fx.top = fx.freeTop()
		res, _ := fx.write(context.Background(), nil)
		if res.Outcome != OutDone {
			t.Fatalf("crash at step %d: the new restore: %s %v", k, res.Outcome, res.Err)
		}
	}
}

// covers: MA-1122, R40, S68, S74
// An error (not a crash) at every step before the commit — a full disk, a failed
// fsync, a refused create — ends the write with no parent file, and its own cleanup
// (the one cleanup) leaves Local Folders as it was and logs "ended" with the result;
// the words say nothing was added and the part written was removed. A Cancel does the
// same with the Stopped words.
func TestRestoreFaultAtEveryStep(t *testing.T) {
	in, msgs := sampleFolders()
	ops := opsOf(t, in, msgs)
	for k := 1; k <= len(ops); k++ {
		if strings.HasPrefix(ops[k-1], "create Restored — Work mail") && ops[k-1] == "create Restored — Work mail" {
			break // the commit and after: TestRestoreParentFsync
		}
		fx := newFixture(t, in, msgs)
		before := fx.local()
		n := 0
		f := &faultFS{inner: newRootFS(fx.root), hook: func(op, name string) error {
			n++
			if n == k {
				return syscallENOSPC()
			}
			return nil
		}}
		res, id := fx.write(context.Background(), f)
		if res.Outcome == OutDone {
			t.Fatalf("fault at step %d (%s): finished", k, ops[k-1])
		}
		if fileExists(filepath.Join(fx.lf.Dir, "Restored — Work mail")) {
			t.Fatalf("fault at step %d: a parent file", k)
		}
		if got := fx.local(); got != before {
			t.Fatalf("fault at step %d (%s): Local Folders not as it was", k, ops[k-1])
		}
		ev := fx.events(id)
		if len(ev) > 0 && ev[len(ev)-1] != EvEnded {
			t.Fatalf("fault at step %d: %v", k, ev)
		}
		if !strings.Contains(strings.ToLower(res.Words), "nothing was added to thunderbird") {
			t.Fatalf("fault at step %d: words %q", k, res.Words)
		}
	}

	fx := newFixture(t, in, msgs)
	before := fx.local()
	ctx, cancel := context.WithCancel(context.Background())
	f := &faultFS{inner: newRootFS(fx.root), hook: func(op, name string) error {
		if op == "create" && strings.HasSuffix(name, "/Sent") {
			cancel()
		}
		return nil
	}}
	res, id := fx.write(ctx, f)
	if res.Outcome != OutStopped || fx.local() != before || dump(fx.events(id)) != "[started ended]" {
		t.Fatalf("Cancel: %s %v", res.Outcome, fx.events(id))
	}
	mustContain(t, res.Words, "The restore was stopped, so nothing was added", "The part already written was removed", "cannot undo a copy")
}

// covers: MA-1122, R40, S68
// Nothing removed, with Local Folders unchanged in and beside the container (assure
// NoSideEffect over the listing and every file's bytes), when the one cleanup finds:
// a foreign file, a Thunderbird-style .msf, a link where a listed entry was, a listed
// file that is now a folder, a made-list that cannot be read; an entry named
// "Restored — NAME" beside the container (a file, a folder, a link) on every live path
// (Cancel, a failure, W-CHECKFAIL); and a "taken" line (step 0b) even with no entry
// there now. Each positive twin — the same state without the foreign thing — is cleaned
// and logged. Prove-fail: a cleanup that removes listed entries before its scan goes red.
func TestRestoreCleanupRemovesNothingWhenNotOnlyItsOwn(t *testing.T) {
	in, msgs := sampleFolders()
	type plant func(fx *fixture, container string)
	foreign := map[string]plant{
		"a foreign file": func(fx *fixture, c string) { writeFile(t, filepath.Join(c, "notes.txt"), "the person's") },
		"a .msf inside":  func(fx *fixture, c string) { writeFile(t, filepath.Join(c, "Inbox.msf"), "summary") },
		"a kind changed": func(fx *fixture, c string) {
			p := filepath.Join(c, "Sent")
			_ = os.Remove(p)
			_ = os.Mkdir(p, 0o700)
		},
		"the list unreadable": func(fx *fixture, c string) {
			l, _ := os.ReadFile(filepath.Join(c, MadeListName))
			_ = os.WriteFile(filepath.Join(c, MadeListName), append([]byte("garbage line\n"), l...), 0o600)
		},
	}
	if runtime.GOOS != "windows" {
		foreign["a link where a listed file was"] = func(fx *fixture, c string) {
			p := filepath.Join(c, "Sent")
			_ = os.Remove(p)
			_ = os.Symlink("/etc/hosts", p)
		}
	}
	for name, pl := range foreign {
		t.Run(name, func(t *testing.T) {
			// A crash after "checked", before the parent file: then the plant, then Start.
			fx := newFixture(t, in, msgs)
			_, crashed := fx.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail"))
			if !crashed {
				t.Fatal("no crash")
			}
			c := filepath.Join(fx.lf.Dir, "Restored — Work mail.sbd")
			pl(fx, c)
			before := fx.local()
			s := fx.settle(false)
			rc, msg := 0, ""
			if len(s) == 1 && s[0].Action == EvAbandoned && s[0].Clean != nil && s[0].Clean.Kind.RemovedNothing() {
				rc, msg = 1, s[0].Words
			}
			refusedAs(t, errFrom(rc, msg), "W-KEPT", func() bool { return fx.local() == before }, "Restored — Work mail.sbd", "delete it yourself")
		})
	}
	t.Run("positive twin: the same state, nothing foreign", func(t *testing.T) {
		fx := newFixture(t, in, msgs)
		before := fx.local()
		if _, crashed := fx.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail")); !crashed {
			t.Fatal("no crash")
		}
		s := fx.settle(false)
		if len(s) != 1 || s[0].Action != EvCleaned || fx.local() != before {
			t.Fatalf("cleaned: %+v", s)
		}
	})

	// Live paths meeting an entry of the parent name.
	for _, kind := range []string{"file", "folder", "link"} {
		for _, path := range []string{"cancel", "failure", "checkfail"} {
			if kind == "link" && runtime.GOOS == "windows" {
				continue
			}
			t.Run("parent "+kind+" at "+path, func(t *testing.T) {
				fx := newFixture(t, in, msgs)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var before string
				planted := false
				f := &faultFS{inner: newRootFS(fx.root), hook: func(op, name string) error {
					trigger := (path != "checkfail" && op == "create" && strings.HasSuffix(name, "/Sent")) ||
						(path == "checkfail" && op == "open" && strings.HasSuffix(name, "/Inbox"))
					if !trigger || planted {
						return nil
					}
					planted = true
					p := filepath.Join(fx.lf.Dir, "Restored — Work mail")
					switch kind {
					case "file":
						writeFile(t, p, "")
					case "folder":
						_ = os.Mkdir(p, 0o700)
					case "link":
						_ = os.Symlink("elsewhere", p)
					}
					if path == "checkfail" {
						_ = os.WriteFile(filepath.Join(fx.lf.Dir, "Restored — Work mail.sbd", "Inbox"), []byte("changed"), 0o600)
					}
					before = fx.local()
					switch path {
					case "cancel":
						cancel()
					case "failure":
						return syscallENOSPC()
					}
					return nil
				}}
				res, id := fx.write(ctx, f)
				rc := 0
				if res.Outcome != OutDone && res.Clean != nil && res.Clean.Kind == CleanParent && dump(fx.events(id)) != "" && lastEv(fx.events(id)) == EvAbandoned {
					rc = 1
				}
				// Everything there when the entry appeared is still there, unchanged (a Cancel
				// may have made one more file before it was seen: that stays too).
				refusedAs(t, errFrom(rc, res.Words), "W-KEPT", func() bool { return keptAll(before, fx.local()) }, "removed nothing", "Restored — Work mail")
			})
		}
	}

	// Step 0b: a "taken" line means remove nothing, even with no entry of the name now.
	t.Run("taken line, entry gone", func(t *testing.T) {
		fx := newFixture(t, in, msgs)
		f := &faultFS{inner: newRootFS(fx.root), hook: func(op, name string) error {
			if op == "create" && name == "Restored — Work mail" {
				return fs.ErrExist // made and removed again by something, in that instant
			}
			return nil
		}}
		var before string
		f2 := &faultFS{inner: f, hook: func(op, name string) error {
			if op == "create" && name == "Restored — Work mail" {
				before = fx.local()
			}
			return nil
		}}
		res, id := fx.write(context.Background(), f2)
		rc := 0
		if res.Outcome == OutTaken && dump(fx.events(id)) == "[started checked taken abandoned]" {
			rc = 1
		}
		refusedAs(t, errFrom(rc, res.Words), "W-KEPT", func() bool { return fx.local() == before }, "made, then removed again", "removed nothing", "Thunderbird does not show")
	})
}

// covers: MA-1122, R40, S68
// Something added between the scan and the removals stops removal part way (step 4):
// what is left stays, with the list and the marker, the log says "abandoned", and the
// words are W-LEFT's step-4 form (foldcheck3 S4), which says part was removed.
func TestRestoreCleanupStopsWhenSomethingAppearsMidRemoval(t *testing.T) {
	in, msgs := sampleFolders()
	fx := newFixture(t, in, msgs)
	if _, crashed := fx.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail")); !crashed {
		t.Fatal("no crash")
	}
	c := filepath.Join(fx.lf.Dir, "Restored — Work mail.sbd")
	f := &faultFS{inner: newRootFS(fx.root), hook: func(op, name string) error {
		if op == "remove" && strings.HasSuffix(name, ".sbd/Projects") {
			writeFile(t, filepath.Join(c, "Inbox.sbd", "added-after-the-scan"), "x")
		}
		return nil
	}}
	s, err := Settle(SettleInput{Log: fx.log, Local: fx.lf, FS: f, ProfileDir: fx.profile})
	if err != nil || len(s) != 1 {
		t.Fatal(err)
	}
	if s[0].Action != EvAbandoned || s[0].Clean.Kind != CleanLeftPart {
		t.Fatalf("stopped part way: %+v", s[0])
	}
	mustContain(t, s[0].Words, "removed part of what it wrote", "found something there it did not make", "removed nothing more")
	if !fileExists(filepath.Join(c, MarkerName)) || !fileExists(filepath.Join(c, MadeListName)) || !fileExists(filepath.Join(c, "Inbox.sbd", "added-after-the-scan")) {
		t.Fatal("the marker, the list and the new entry stay")
	}

	// Something new directly in the container, after every listed entry is gone: the
	// list and the marker still stay (they go only once the container holds nothing
	// else), so backups keep leaving the folder out.
	fx2 := newFixture(t, in, msgs)
	if _, crashed := fx2.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail")); !crashed {
		t.Fatal("no crash")
	}
	c2 := filepath.Join(fx2.lf.Dir, "Restored — Work mail.sbd")
	once := false
	f2 := &faultFS{inner: newRootFS(fx2.root), hook: func(op, name string) error {
		if op == "remove" && !once {
			once = true
			writeFile(t, filepath.Join(c2, "new-at-the-top"), "x")
		}
		return nil
	}}
	s2, err := Settle(SettleInput{Log: fx2.log, Local: fx2.lf, FS: f2, ProfileDir: fx2.profile})
	if err != nil || len(s2) != 1 || s2[0].Clean.Kind != CleanLeftPart {
		t.Fatalf("stopped before the list and the marker: %+v %v", s2, err)
	}
	if !fileExists(filepath.Join(c2, MarkerName)) || !fileExists(filepath.Join(c2, MadeListName)) {
		t.Fatal("the marker and the list stay while anything else is in the container")
	}
}

// covers: MA-1122, R40, S68
// The made-list's edges: a torn last line (a power cut mid-append) is passed over and
// the cleanup still removes everything; a container with no marker (a crash between the
// Mkdir and the marker) is left whole and the next restore takes " (2)" (N1).
func TestRestoreMadeListEdges(t *testing.T) {
	in, msgs := sampleFolders()
	fx := newFixture(t, in, msgs)
	before := fx.local()
	if _, crashed := fx.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail")); !crashed {
		t.Fatal("no crash")
	}
	lp := filepath.Join(fx.lf.Dir, "Restored — Work mail.sbd", MadeListName)
	fh, err := os.OpenFile(lp, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fh.WriteString(`{"k":"f","p":"Ne`)
	fh.Close()
	if s := fx.settle(false); len(s) != 1 || s[0].Action != EvCleaned || fx.local() != before {
		t.Fatalf("a torn last line is passed over: %+v", s)
	}

	fx2 := newFixture(t, in, msgs)
	if _, crashed := fx2.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail.sbd/"+MarkerName)); !crashed {
		t.Fatal("no crash")
	}
	s := fx2.settle(false)
	if len(s) != 1 || s[0].Case != "1" || s[0].Action != EvAbandoned || !fileExists(filepath.Join(fx2.lf.Dir, "Restored — Work mail.sbd")) {
		t.Fatalf("a container without a marker is left: %+v", s)
	}
	fx2.top = fx2.freeTop()
	if fx2.top != "Restored — Work mail (2)" {
		t.Fatalf("the next restore takes (2): %q", fx2.top)
	}
}

// covers: MA-1122, R40, S68
// The parent file's fsync failing after a successful create (foldcheck2 N-c): the one
// re-check finds a regular, empty file → the restore counts as committed, logs
// "ended" and names the error; anything else → "abandoned", nothing removed,
// W-COMMITERR.
func TestRestoreParentFsync(t *testing.T) {
	in, msgs := sampleFolders()
	for _, recheck := range []bool{true, false} {
		fx := newFixture(t, in, msgs)
		f := &faultFS{inner: newRootFS(fx.root), hook: func(op, name string) error {
			if op == "sync" && name == "Restored — Work mail" {
				if !recheck {
					_ = os.WriteFile(filepath.Join(fx.lf.Dir, "Restored — Work mail"), []byte("x"), 0o600)
				}
				return errors.New("the disk did not confirm the write")
			}
			return nil
		}}
		res, id := fx.write(context.Background(), f)
		if recheck {
			if res.Outcome != OutDone || res.AfterErr == nil || dump(fx.events(id)) != "[started checked ended]" {
				t.Fatalf("re-check passes: %s %v %v", res.Outcome, res.AfterErr, fx.events(id))
			}
			mustContain(t, res.Words, "Restored 5 messages", "the disk did not confirm")
			continue
		}
		if res.Outcome != OutCommitErr || dump(fx.events(id)) != "[started checked abandoned]" || !fileExists(filepath.Join(fx.lf.Dir, "Restored — Work mail.sbd", "Inbox")) {
			t.Fatalf("re-check fails: %s %v", res.Outcome, fx.events(id))
		}
		mustContain(t, res.Words, "could not make sure", "removed nothing", "the disk did not confirm")
	}
}

// covers: MA-1122, R40, S68
// The scan matches names as the file system does (foldcheck3 S8): on a volume that
// stores names decomposed (HFS+, NFD), a folder MailArchive created as "Café" reads back
// decomposed and is still the list's, so the leftover is cleaned, not left as foreign;
// but a second entry that spells the same is not the list's (a decomposed twin on a
// volume that keeps both), so nothing is removed.
func TestRestoreScanMatchesNamesAsTheFileSystemDoes(t *testing.T) {
	in := []FolderIn{{Names: []string{"Café"}, Messages: 1}, {Names: []string{"Ünïcödé"}, Messages: 1}}
	m := []byte("Subject: c\n\nx\n")
	fx := newFixture(t, in, [][][]byte{{m}, {m}})
	before := fx.local()
	if _, crashed := fx.writeCrashing(stepOf(t, in, [][][]byte{{m}, {m}}, "create Restored — Work mail")); !crashed {
		t.Fatal("no crash")
	}
	nfd := &nfdFS{inner: newRootFS(fx.root)}
	s, err := Settle(SettleInput{Log: fx.log, Local: fx.lf, FS: nfd, ProfileDir: fx.profile})
	if err != nil || len(s) != 1 || s[0].Action != EvCleaned || fx.local() != before {
		t.Fatalf("names read back decomposed are the list's: %+v %v", s, err)
	}

	scanned := []madeEntry{{K: "f", P: "Café"}}
	twin := &twinFS{names: []string{"Café", norm.NFD.String("Café")}}
	if _, _, clean := scanContainer(twin, "c", scanned, nil); clean {
		t.Fatal("a second entry spelling the same name is not the list's")
	}
}

// nfdFS stands for an HFS+ volume: it returns every name decomposed from ReadDir and
// finds a name whatever its normalisation.
type nfdFS struct{ inner FS }

func (n *nfdFS) Mkdir(p string) error                 { return n.inner.Mkdir(norm.NFC.String(p)) }
func (n *nfdFS) Create(p string) (wfile, error)       { return n.inner.Create(norm.NFC.String(p)) }
func (n *nfdFS) Open(p string) (io.ReadCloser, error) { return n.inner.Open(norm.NFC.String(p)) }
func (n *nfdFS) Lstat(p string) (fs.FileInfo, error)  { return n.inner.Lstat(norm.NFC.String(p)) }
func (n *nfdFS) RemoveSeen(p string, seen map[string]fs.FileInfo) error {
	return n.inner.RemoveSeen(norm.NFC.String(p), seen)
}
func (n *nfdFS) SyncDir(p string) error { return n.inner.SyncDir(norm.NFC.String(p)) }
func (n *nfdFS) ReadDir(p string, fn func([]fs.DirEntry) bool) error {
	return n.inner.ReadDir(norm.NFC.String(p), func(b []fs.DirEntry) bool {
		out := make([]fs.DirEntry, len(b))
		for i, e := range b {
			out[i] = renamed{e, norm.NFD.String(e.Name())}
		}
		return fn(out)
	})
}

type renamed struct {
	fs.DirEntry
	name string
}

func (r renamed) Name() string { return r.name }

// twinFS lists its names in one folder, every one a regular file.
type twinFS struct {
	FS
	names []string
}

func (t *twinFS) ReadDir(_ string, fn func([]fs.DirEntry) bool) error {
	var b []fs.DirEntry
	for _, n := range t.names {
		b = append(b, fakeEntry(n))
	}
	fn(b)
	return nil
}

// Lstat: the container itself is a folder (so the scan reaches its names), every name
// in it a regular file.
func (t *twinFS) Lstat(p string) (fs.FileInfo, error) {
	if !strings.Contains(p, "/") {
		return fakeDir(p), nil
	}
	return fakeInfo(path.Base(p)), nil
}

type fakeDir string

func (d fakeDir) Name() string       { return string(d) }
func (d fakeDir) Size() int64        { return 0 }
func (d fakeDir) Mode() fs.FileMode  { return fs.ModeDir | 0o700 }
func (d fakeDir) ModTime() time.Time { return time.Time{} }
func (d fakeDir) IsDir() bool        { return true }
func (d fakeDir) Sys() any           { return nil }

type fakeEntry string

func (e fakeEntry) Name() string               { return string(e) }
func (e fakeEntry) IsDir() bool                { return false }
func (e fakeEntry) Type() fs.FileMode          { return 0 }
func (e fakeEntry) Info() (fs.FileInfo, error) { return fakeInfo(e), nil }

type fakeInfo string

func (i fakeInfo) Name() string       { return string(i) }
func (i fakeInfo) Size() int64        { return 1 }
func (i fakeInfo) Mode() fs.FileMode  { return 0o600 }
func (i fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool        { return false }
func (i fakeInfo) Sys() any           { return nil }
