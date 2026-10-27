package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/memguard"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// The engine's own tests inject faults through a SealedCopy over in-memory
// plaintext (CLAUDE.md: mocks for fault injection only). The copy of a real
// encrypted archive through the key is internal/keyring's DecryptCopy test, and
// byte-for-byte against MA-329's golden the Phase 2c closure (package app_test).

type nopPersister struct{}

func (nopPersister) Commit(state.View) error { return nil }

// fakeMsg is one message the fake archive holds: its files' plaintext by suffix.
type fakeMsg struct {
	path     string
	files    map[string][]byte // ".html", "-attachments.zip", ".eml"
	missing  []string          // the record's Missing labels: an issue the report lists
	fallback string            // the record's BodyFallback
}

type fakeCopy struct {
	t        *testing.T
	out      string
	dest     string // the copy's folder, once planned
	m        *state.Manifest
	plain    map[string][]byte // unit+kind → plaintext served
	fail     map[string]error  // rel path → the error its Unit returns
	during   func(rel string)  // runs inside Unit, before the plaintext is used
	history  []state.HistoryEvent
	lost     int
	histErr  error
	image    []byte
	imageErr error
	gap      bool   // the record marks part of the timeline lost
	behind   bool   // the sealed index is marked behind
	onLoad   func() // runs inside Load
	onHist   func() // runs inside History
	onImage  func() // runs inside IndexImage
}

func unitID(i int) string { return fmt.Sprintf("%032x", i+1) }

func digest(b []byte) *state.FileDigest {
	s := sha256.Sum256(b)
	return &state.FileDigest{SHA256: hex.EncodeToString(s[:]), Size: int64(len(b))}
}

func sealedOf(b []byte) *state.SealedUnit {
	s := sha256.Sum256(append([]byte("sealed"), b...))
	return &state.SealedUnit{Rev: 1, SHA256: hex.EncodeToString(s[:]), Size: int64(len(b)) + 40}
}

var copyKinds = []struct {
	suffix string
	kind   vault.UnitKind
}{{".html", vault.UnitMessage}, {"-attachments.zip", vault.UnitAttachment}, {".eml", vault.UnitOriginal}}

// newFakeCopy is an encrypted archive's sealed side holding msgs, over a real
// encrypted archive folder (so the gate and the lock are the real ones).
func newFakeCopy(t *testing.T, msgs ...fakeMsg) *fakeCopy {
	t.Helper()
	out := sealedArchive(t)
	f := &fakeCopy{t: t, out: out, plain: map[string][]byte{}, fail: map[string]error{}}
	entries := map[string]state.Record{}
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	for i, m := range msgs {
		r := state.Record{Path: m.path, Folder: "Inbox", ExportedAt: at, FirstSeen: at, LastSeen: at, FirstFolder: "Inbox", Present: true,
			Unit: unitID(i), Sealed: &state.SealedSet{}, Fixity: &state.Fixity{}, Missing: m.missing, BodyFallback: m.fallback}
		for _, k := range copyKinds {
			b, ok := m.files[k.suffix]
			if !ok {
				continue
			}
			f.plain[fmt.Sprintf("%s/%d", r.Unit, k.kind)] = b
			switch k.suffix {
			case ".html":
				r.Sealed.HTML, r.Fixity.HTML = sealedOf(b), digest(b)
			case "-attachments.zip":
				r.Sealed.Zip, r.Fixity.Zip = sealedOf(b), digest(b)
			default:
				r.Sealed.EML, r.Fixity.EML = sealedOf(b), digest(b)
			}
		}
		entries[fmt.Sprintf("s\x00Inbox\x00m%d", i)] = r
	}
	m, err := state.FromSealed(filepath.Join(out, ".mailarchive-manifest.json"), state.View{Version: 6, Entries: entries, Stores: map[string]string{"path:/mail/a.mbox": "s"}}, nopPersister{})
	if err != nil {
		t.Fatal(err)
	}
	f.m = m
	ix, _ := index.OpenMemory(nil)
	f.image, _ = ix.Image()
	ix.Close()
	f.history = []state.HistoryEvent{{Run: 1, At: at.Format(time.RFC3339)}, {K: "s\x00Inbox\x00m0", Folder: "Inbox"}}
	return f
}

func (f *fakeCopy) Load() (*state.Manifest, error) {
	if f.onLoad != nil {
		f.onLoad()
	}
	return f.m, nil
}

func (f *fakeCopy) Unit(ctx context.Context, rec state.Record, kind vault.UnitKind, su state.SealedUnit, use func([]byte) error) error {
	stem := strings.TrimSuffix(rec.Path, ".html")
	for _, k := range copyKinds {
		if k.kind != kind {
			continue
		}
		rel := stem + k.suffix
		if f.during != nil {
			f.during(rel)
		}
		if err := f.fail[rel]; err != nil {
			return err
		}
	}
	b, ok := f.plain[fmt.Sprintf("%s/%d", rec.Unit, kind)]
	if !ok {
		return fs.ErrNotExist
	}
	return use(bytes.Clone(b))
}

func (f *fakeCopy) History() ([]state.HistoryEvent, int, error) {
	if f.onHist != nil {
		f.onHist()
	}
	return f.history, f.lost, f.histErr
}
func (f *fakeCopy) HistoryGap() bool  { return f.gap }
func (f *fakeCopy) IndexBehind() bool { return f.behind }
func (f *fakeCopy) IndexImage(w io.Writer) error {
	if f.onImage != nil {
		f.onImage()
	}
	if f.imageErr != nil {
		return f.imageErr
	}
	_, err := w.Write(f.image)
	return err
}
func (f *fakeCopy) IndexSize() int64 { return int64(len(f.image)) }

func copyMsg(name string) fakeMsg {
	return fakeMsg{path: "s/Inbox/" + name + ".html", files: map[string][]byte{
		".html":            []byte("<html><body>" + name + "</body></html>"),
		"-attachments.zip": []byte("PK zip of " + name),
		".eml":             []byte("Subject: " + name + "\r\n\r\nbody\r\n"),
	}}
}

// copyOf plans and makes the copy of f into a fresh parent.
func copyOf(t *testing.T, f *fakeCopy, ctx context.Context, logger *log.Logger) (DecryptReport, *DecryptPlan, error) {
	t.Helper()
	e := planEnv()
	p, err := e.PlanDecryptCopy(f.out, tmpDir(t), nil, nil, planDay)
	if err != nil {
		t.Fatal(err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	f.dest = p.Dest
	rep, err := e.DecryptSealed(ctx, p, nil, nil, f, logger, nil)
	return rep, p, err
}

// covers: MA-820, R37
// A whole copy (phase 3 §10): every message's files written at their recorded
// paths with their plaintext, the history, search.db from the sealed image, the
// pages, report and README, and a plaintext manifest — no unit placement, written
// last — and the unfinished mark removed; exit 0; nothing written into the archive;
// the log says counts only, never a path or a name. The lock was taken with no verb.
func TestAWholeCopyIsAPlaintextArchive(t *testing.T) {
	f := newFakeCopy(t, copyMsg("alpha"), copyMsg("beta"))
	before := snapshotTree(t, f.out)
	var verb string
	orig := copyLock
	copyLock = func(path string) (*lockfile.Lock, error) {
		l, err := orig(path)
		b, _ := os.ReadFile(path)
		verb = string(b)
		return l, err
	}
	defer func() { copyLock = orig }()
	var logged bytes.Buffer
	rep, p, err := copyOf(t, f, nil, log.New(&logged, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	if rep.ExitCode() != 0 || rep.Copied != 2 || rep.Skipped != 0 || rep.Unfinished || rep.Index != "copied" {
		t.Fatalf("a whole copy reported %+v", rep)
	}
	for _, m := range []fakeMsg{copyMsg("alpha"), copyMsg("beta")} {
		stem := filepath.Join(p.Dest, filepath.FromSlash(strings.TrimSuffix(m.path, ".html")))
		for suffix, want := range m.files {
			if got, err := os.ReadFile(stem + suffix); err != nil || !bytes.Equal(got, want) {
				t.Fatalf("%s%s: %q %v", m.path, suffix, got, err)
			}
		}
	}
	for _, n := range []string{state.HistoryName, "search.db", "README.txt", "index.html", ".mailarchive-manifest.json"} {
		if _, err := os.Stat(filepath.Join(p.Dest, n)); err != nil {
			t.Fatalf("the copy lacks %s: %v", n, err)
		}
	}
	for _, n := range []string{CopyMarkerName, CopyProblemsName, vault.FileName, vault.BackupName} {
		if _, err := os.Stat(filepath.Join(p.Dest, n)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("a whole copy holds %s", n)
		}
	}
	m, err := state.Load(filepath.Join(p.Dest, ".mailarchive-manifest.json"))
	if err != nil || m.Len() != 2 || m.HasUnits() {
		t.Fatalf("the copy's manifest: %v, %d records, units %v", err, m.Len(), m.HasUnits())
	}
	if fi, _ := os.Stat(p.Dest); fi.Mode().Perm() != 0o700 {
		t.Fatalf("the copy's folder is %v, want 0700", fi.Mode().Perm())
	}
	if !sameTree(withoutLock(before), withoutLock(snapshotTree(t, f.out))) {
		t.Fatal("the copy wrote into the archive")
	}
	if s := logged.String(); strings.Contains(s, "alpha") || strings.Contains(s, "Inbox") || strings.Contains(s, p.Dest) || !strings.Contains(s, "2 of 2") {
		t.Fatalf("the log names a message or a folder, or not the counts: %q", s)
	}
	if verb == "" || strings.Contains(verb, "verb=") {
		t.Fatalf("the lock was not taken, or names a verb: %q", verb)
	}
}

// covers: MA-820, R37
// A copy that leaves messages out finishes the rest (phase 3 §16, the implementer's
// call): each message whose file is missing, damaged, too large, does not match its
// recorded checksum, collides with a name already in the copy, has a path that is not
// safe, or takes a name the copy keeps for itself, is listed with why in the problems
// file inside the copy — never in the log — and the copy exits 3, partial, its
// unfinished mark removed. The rest are copied whole.
func TestACopyListsWhatItLeftOutAndExitsPartial(t *testing.T) {
	good := copyMsg("good")
	collide := copyMsg("Twin")
	collide.files = map[string][]byte{".html": []byte("<html>Twin</html>")}
	twin := fakeMsg{path: "s/Inbox/twin.html", files: map[string][]byte{".html": []byte("<html>twin</html>")}}
	unsafe := fakeMsg{path: "s/../escape.html", files: map[string][]byte{".html": []byte("x")}}
	reserved := fakeMsg{path: "s/Inbox/index.html", files: map[string][]byte{".html": []byte("x")}}
	dotted := fakeMsg{path: "s/.hidden/m.html", files: map[string][]byte{".html": []byte("x")}}
	top := fakeMsg{path: "search.db/m.html", files: map[string][]byte{".html": []byte("x")}}
	f := newFakeCopy(t, good, copyMsg("missing"), copyMsg("damaged"), copyMsg("large"), copyMsg("tampered"), collide, twin, unsafe, reserved, dotted, top)
	f.fail["s/Inbox/missing-attachments.zip"] = fs.ErrNotExist
	f.fail["s/Inbox/damaged.eml"] = fmt.Errorf("%w: the tag", ErrUnitDoesNotOpen)
	f.fail["s/Inbox/large.html"] = memguard.ErrTooLarge
	for k, b := range f.plain {
		if strings.Contains(string(b), "tampered</body>") {
			f.plain[k] = []byte("<html><body>someone else</body></html>")
		}
	}
	var logged bytes.Buffer
	rep, p, err := copyOf(t, f, nil, log.New(&logged, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	caseInsensitive := false
	if _, err := os.Stat(filepath.Join(p.Dest, "s", "Inbox", "TWIN.html")); err == nil {
		caseInsensitive = true
	}
	wantSkipped := 9
	if !caseInsensitive {
		wantSkipped = 8
	}
	if rep.ExitCode() != 3 || rep.Skipped != wantSkipped || rep.Copied != 11-wantSkipped || rep.Unfinished {
		t.Fatalf("a partial copy reported %+v", rep)
	}
	problems, err := os.ReadFile(filepath.Join(p.Dest, CopyProblemsName))
	if err != nil {
		t.Fatal(err)
	}
	for rel, why := range map[string]string{
		"s/Inbox/missing-attachments.zip": "missing from the archive",
		"s/Inbox/damaged.eml":             "damaged",
		"s/Inbox/large.html":              "memory",
		"s/Inbox/tampered.html":           "checksum",
		"s/../escape.html":                "not a safe path",
		"s/Inbox/index.html":              "keeps for its own files",
		"s/.hidden/m.html":                "keeps for its own files",
		"search.db/m.html":                "keeps for its own files",
	} {
		if !strings.Contains(string(problems), rel+": ") || !strings.Contains(string(problems), why) {
			t.Errorf("the problems file does not list %s (%s):\n%s", rel, why, problems)
		}
	}
	if caseInsensitive && !strings.Contains(string(problems), "same name") {
		t.Errorf("on a case-insensitive system the twin names are not listed:\n%s", problems)
	}
	if b, _ := os.ReadFile(filepath.Join(p.Dest, "s", "Inbox", "good.eml")); !bytes.Equal(b, good.files[".eml"]) {
		t.Fatal("a good message beside the problems was not copied whole")
	}
	if _, err := os.Stat(filepath.Join(p.Dest, CopyMarkerName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("a partial copy kept its unfinished mark: making it again cannot mend damage")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(p.Dest), "escape.html")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("an unsafe path was written outside the copy")
	}
	if s := logged.String(); strings.Contains(s, "missing") || strings.Contains(s, "Inbox") {
		t.Fatalf("the log names a message: %q", s)
	}
}

// covers: MA-820, R37
// A lock of the key mid-copy stops it as ErrCopyLocked, never as damage, and a stop
// as ErrCopyStopped; either way the folder keeps its unfinished mark, rewritten with
// counts only, and holds no manifest — so nothing takes it for an archive. A failure
// to write the copy (a full disk) fails it the same way.
func TestAStoppedCopyStaysMarkedUnfinished(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(f *fakeCopy, cancel func())
		want  error
	}{
		{"locked", func(f *fakeCopy, _ func()) {
			f.fail["s/Inbox/b.html"] = fmt.Errorf("%w: the key", ErrCopyLocked)
		}, ErrCopyLocked},
		{"stopped", func(f *fakeCopy, cancel func()) {
			f.during = func(rel string) {
				if rel == "s/Inbox/b.html" {
					cancel()
				}
			}
			f.fail["s/Inbox/b.html"] = context.Canceled
		}, ErrCopyStopped},
		{"history locked", func(f *fakeCopy, _ func()) {
			f.histErr = fmt.Errorf("%w: the key", ErrCopyLocked)
		}, ErrCopyLocked},
		{"index locked", func(f *fakeCopy, _ func()) {
			f.imageErr = fmt.Errorf("%w: the key", ErrCopyLocked)
		}, ErrCopyLocked},
		{"a write fails", func(f *fakeCopy, _ func()) {
			f.fail["s/Inbox/b.html"] = errors.New("no space left on device")
		}, ErrCopyFailed},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeCopy(t, copyMsg("a"), copyMsg("b"), copyMsg("c"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.setup(f, cancel)
			rep, p, err := copyOf(t, f, ctx, nil)
			if !errors.Is(err, c.want) || (c.want != ErrCopyFailed && errors.Is(err, ErrCopyFailed)) {
				t.Fatalf("want %v alone, got %v", c.want, err)
			}
			if !rep.Unfinished {
				t.Fatal("the report does not say unfinished")
			}
			mark, merr := os.ReadFile(filepath.Join(p.Dest, CopyMarkerName))
			if merr != nil || !strings.Contains(string(mark), "UNENCRYPTED") || !strings.Contains(string(mark), "make the copy again") || strings.Contains(string(mark), "Inbox") {
				t.Fatalf("the unfinished mark: %q %v", mark, merr)
			}
			if _, err := os.Stat(filepath.Join(p.Dest, ".mailarchive-manifest.json")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("an unfinished copy holds a manifest")
			}
			assure.Reached(t, strings.Contains(err.Error(), p.Dest), "the error names the unfinished folder")
		})
	}
}

// covers: MA-820, R37
// search.db comes from the sealed image when it is one; an image that cannot be used
// is replaced by an index rebuilt from the copied files, every message found by it;
// the copy is whole either way.
func TestACopysIndexIsTheImageOrRebuilt(t *testing.T) {
	// Bytes that are not a database, and an empty image — which would open as a new
	// index that finds nothing — are both replaced by a rebuilt index.
	for label, image := range map[string][]byte{"not a database": []byte("not a database at all"), "empty": {}} {
		f := newFakeCopy(t, copyMsg("alpha"))
		f.image = image
		rep, p, err := copyOf(t, f, nil, nil)
		if err != nil || rep.Index != "rebuilt" || rep.ExitCode() != 0 {
			t.Fatalf("%s: %+v %v", label, rep, err)
		}
		ix, err := index.Open(filepath.Join(p.Dest, "search.db"))
		if err != nil {
			t.Fatal(err)
		}
		n, _ := ix.Count()
		ix.Close()
		if n != 1 {
			t.Fatalf("%s: the rebuilt index holds %d rows, want 1", label, n)
		}
	}
}

// covers: MA-820, R37
// A stop is seen between messages: the message after it is never written.
func TestAStopIsSeenBetweenMessages(t *testing.T) {
	f := newFakeCopy(t, copyMsg("a"), copyMsg("b"), copyMsg("c"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.during = func(rel string) {
		if rel == "s/Inbox/b.eml" {
			cancel()
		}
	}
	rep, p, err := copyOf(t, f, ctx, nil)
	_, cerr := os.Stat(filepath.Join(p.Dest, "s", "Inbox", "c.html"))
	assure.Reached(t, errors.Is(err, ErrCopyStopped) && rep.Unfinished && os.IsNotExist(cerr), "stopped before the next message")
}

// covers: MA-820, R37
// A link or a special file found in the copy before its mark is removed fails it:
// the mark stays and the error names it (only this user could plant one — the folder
// is 0700 — but the check runs).
func TestALinkInTheCopyKeepsItUnfinished(t *testing.T) {
	f := newFakeCopy(t, copyMsg("a"), copyMsg("b"))
	f.during = func(rel string) {
		if rel == "s/Inbox/b.html" {
			os.Symlink(os.TempDir(), filepath.Join(f.dest, "s", "Inbox", "link"))
		}
	}
	rep, p, err := copyOf(t, f, nil, nil)
	if !errors.Is(err, ErrCopyFailed) || !rep.Unfinished || !strings.Contains(err.Error(), "link") {
		t.Fatalf("want a failure naming the link, got %+v %v", rep, err)
	}
	if _, err := os.Stat(filepath.Join(p.Dest, CopyMarkerName)); err != nil {
		t.Fatal("the mark was removed")
	}
}

// covers: MA-820, R37
// On a read-only medium the copy is made without the archive's lock, and the record
// is compared before and after: an archive that changed meanwhile (another computer's
// backup through another path) leaves the copy marked unfinished; one that did not
// is copied whole. A destination without room is refused before anything is made.
func TestACopyOffAReadOnlyMediumAndOneWithoutRoom(t *testing.T) {
	orig := copyLock
	copyLock = func(path string) (*lockfile.Lock, error) {
		return nil, &fs.PathError{Op: "open", Path: path, Err: syscall.EROFS}
	}
	defer func() { copyLock = orig }()
	f := newFakeCopy(t, copyMsg("a"))
	if rep, _, err := copyOf(t, f, nil, nil); err != nil || rep.Unfinished {
		t.Fatalf("positive twin: a read-only archive copied lock-free: %+v %v", rep, err)
	}
	f = newFakeCopy(t, copyMsg("a"), copyMsg("b"))
	mirror := filepath.Join(f.out, container.MirrorName)
	f.during = func(rel string) {
		if rel == "s/Inbox/b.html" {
			b, _ := os.ReadFile(mirror)
			os.WriteFile(mirror, append(b, ' '), 0o600)
		}
	}
	rep, p, err := copyOf(t, f, nil, nil)
	if !errors.Is(err, ErrCopyFailed) || !rep.Unfinished || !strings.Contains(err.Error(), "changed while") {
		t.Fatalf("an archive that changed during a lock-free copy: %+v %v", rep, err)
	}
	if _, serr := os.Stat(filepath.Join(p.Dest, CopyMarkerName)); serr != nil {
		t.Fatal("the copy of an archive that changed lost its unfinished mark")
	}
	copyLock = orig

	f = newFakeCopy(t, copyMsg("a"))
	e := planEnv()
	parent := tmpDir(t)
	plan, err := e.PlanDecryptCopy(f.out, parent, nil, nil, planDay)
	if err != nil {
		t.Fatal(err)
	}
	e.freeDest = func(string) (uint64, error) { return 10, nil }
	_, err = e.DecryptSealed(context.Background(), plan, nil, nil, f, nil, nil)
	var r *CopyRefusal
	entries, _ := os.ReadDir(parent)
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(parent, "free"),
		assure.NoSideEffect(func() bool { return errors.As(err, &r) && r.ID == "R-g2" && len(entries) == 0 }))
}

// covers: MA-820, R37
// A copy left unfinished is never backed up into — no new mail is mixed into it —
// and status says what it is, in red (phase 3 §16).
func TestAnUnfinishedCopyIsNotBackedUpIntoAndStatusSaysSo(t *testing.T) {
	f := newFakeCopy(t, copyMsg("a"), copyMsg("b"))
	f.fail["s/Inbox/b.html"] = fmt.Errorf("%w: the key", ErrCopyLocked)
	_, p, err := copyOf(t, f, nil, nil)
	if !errors.Is(err, ErrCopyLocked) {
		t.Fatal(err)
	}
	before := snapshotTree(t, p.Dest)
	_, err = Run(context.Background(), Options{Inputs: []string{encryptedSourcePST(t)}, Out: p.Dest}, discard(), nil)
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(p.Dest, "did not finish"),
		assure.NoSideEffect(func() bool { return sameTree(before, snapshotTree(t, p.Dest)) }))
	in := health.Gather(p.Dest, "")
	hr := health.Assess(in, time.Now())
	assure.Reached(t, hr.Posture == "RED" && strings.Contains(strings.Join(hr.Codes, " "), "unfinished_copy"), "status names the unfinished copy in red")
}
