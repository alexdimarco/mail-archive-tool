package tbrestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// A writer fixture: a fake profile in a temp folder with its Local Folders, a
// restores.log in a temp folder, and a small archive's folders and messages (CRLF and
// LF messages, "From " and ">From " lines, a final line with no newline).

type fixture struct {
	t       *testing.T
	profile string
	lf      LocalFolders
	root    *os.Root
	log     *Log
	layout  *Layout
	top     string
	msgs    [][][]byte // per layout folder
	enc     bool
}

// sampleFolders are an archive's folders: Inbox (with Receipts below it), Sent, and
// an empty parent with a child.
func sampleFolders() ([]FolderIn, [][][]byte) {
	crlf := []byte("From: Ann <ann@example.org>\r\nDate: Mon, 2 Nov 2026 10:00:00 +0000\r\nSubject: one\r\n\r\nFrom here a line\r\n>From there\r\nend\r\n")
	lf := []byte("From: Bob <bob@example.org>\nSubject: two\n\nbody line\n>>From deep\nno final newline")
	plain := []byte("Subject: three\r\n\r\nplain\r\n\r\n")
	empty := []byte("")
	in := []FolderIn{
		{Names: []string{"Inbox"}, Messages: 2},
		{Names: []string{"Inbox", "Receipts"}, Messages: 1},
		{Names: []string{"Sent"}, Messages: 1},
		{Names: []string{"Projects"}, Messages: 0},
		{Names: []string{"Projects", "Q1: Reports"}, Messages: 1},
	}
	msgs := [][][]byte{{crlf, lf}, {plain}, {empty}, nil, {crlf}}
	return in, msgs
}

func newFixture(t *testing.T, in []FolderIn, msgs [][][]byte) *fixture {
	t.Helper()
	profile, _ := localProfile(t)
	p, err := ReadPrefs(profile)
	if err != nil {
		t.Fatal(err)
	}
	lf, err := FindLocalFolders(profile, p)
	if err != nil {
		t.Fatal(err)
	}
	// Thunderbird's own folders, which the restore must never touch.
	writeFile(t, filepath.Join(lf.Dir, "Trash"), "From - x\nSubject: old\n\nkept\n")
	writeFile(t, filepath.Join(lf.Dir, "Trash.msf"), "summary")
	root, err := OpenLocalRoot(lf)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	layout, err := PlanLayout("Work mail", in, lf.Dir, "")
	if err != nil {
		t.Fatal(err)
	}
	fx := &fixture{t: t, profile: profile, lf: lf, root: root, log: NewLog(filepath.Join(t.TempDir(), LogName)), layout: layout, msgs: msgs}
	fx.top = fx.freeTop()
	return fx
}

func (fx *fixture) freeTop() string {
	entries, err := os.ReadDir(fx.lf.Dir)
	if err != nil {
		fx.t.Fatal(err)
	}
	top, _, err := FreeTopName(fx.layout.Top, entries)
	if err != nil {
		fx.t.Fatal(err)
	}
	return top
}

func (fx *fixture) input(id string) WriteInput {
	return WriteInput{
		ID: id, ProfileDir: fx.profile, Local: fx.lf, Archive: "/archives/Work mail", Encrypted: fx.enc,
		Layout: fx.layout, Top: fx.top, Log: fx.log,
		Messages: func(i int, emit func([]byte) error) error {
			for _, m := range fx.msgs[i] {
				if err := emit(m); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func (fx *fixture) write(ctx context.Context, f FS) (*WriteResult, string) {
	fx.t.Helper()
	id, err := NewID()
	if err != nil {
		fx.t.Fatal(err)
	}
	if f == nil {
		f = newRootFS(fx.root)
	}
	return Write(ctx, f, fx.input(id)), id
}

func (fx *fixture) settle(readOnly bool) []Settled {
	fx.t.Helper()
	out, err := Settle(SettleInput{Log: fx.log, Local: fx.lf, FS: newRootFS(fx.root), ProfileDir: fx.profile, ReadOnly: readOnly})
	if err != nil {
		fx.t.Fatal(err)
	}
	return out
}

// events are the log's events for one restore, in order.
func (fx *fixture) events(id string) []string {
	fx.t.Helper()
	lines, err := fx.log.Read()
	if err != nil {
		fx.t.Fatal(err)
	}
	var out []string
	for _, l := range lines {
		if l.ID == id {
			out = append(out, l.Event)
		}
	}
	return out
}

func (fx *fixture) local() string { return snapshot(fx.t, fx.lf.Dir) }

func (fx *fixture) logBytes() string {
	b, err := os.ReadFile(fx.log.Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		fx.t.Fatal(err)
	}
	return string(b)
}

// faultFS is the fault seam (the ceFS seam's shape): hook is called before every
// operation with its name and path; an error it returns is the operation's, and a
// crash it panics with stops the write as a power cut would.
type faultFS struct {
	inner FS
	hook  func(op, name string) error
}

type crash struct{}

func (f *faultFS) at(op, name string) error {
	if f.hook != nil {
		return f.hook(op, name)
	}
	return nil
}

func (f *faultFS) Mkdir(n string) error {
	if err := f.at("mkdir", n); err != nil {
		return err
	}
	return f.inner.Mkdir(n)
}

func (f *faultFS) Create(n string) (wfile, error) {
	if err := f.at("create", n); err != nil {
		return nil, err
	}
	w, err := f.inner.Create(n)
	if err != nil {
		return nil, err
	}
	return &faultFile{w: w, f: f, name: n}, nil
}

func (f *faultFS) Open(n string) (io.ReadCloser, error) {
	if err := f.at("open", n); err != nil {
		return nil, err
	}
	return f.inner.Open(n)
}

func (f *faultFS) Lstat(n string) (fs.FileInfo, error) { return f.inner.Lstat(n) }

func (f *faultFS) ReadDir(n string, fn func([]fs.DirEntry) bool) error {
	if err := f.at("readdir", n); err != nil {
		return err
	}
	return f.inner.ReadDir(n, fn)
}

func (f *faultFS) RemoveSeen(n string, seen map[string]fs.FileInfo) error {
	if err := f.at("remove", n); err != nil {
		return err
	}
	return f.inner.RemoveSeen(n, seen)
}

func (f *faultFS) SyncDir(n string) error {
	if err := f.at("syncdir", n); err != nil {
		return err
	}
	return f.inner.SyncDir(n)
}

type faultFile struct {
	w    wfile
	f    *faultFS
	name string
}

func (w *faultFile) Write(p []byte) (int, error) {
	if err := w.f.at("write", w.name); err != nil {
		return 0, err
	}
	return w.w.Write(p)
}

func (w *faultFile) Sync() error {
	if err := w.f.at("sync", w.name); err != nil {
		return err
	}
	return w.w.Sync()
}

func (w *faultFile) Close() error { return w.w.Close() }

// opsOf counts a clean run's operations, by "op name" key in order.
func opsOf(t *testing.T, in []FolderIn, msgs [][][]byte) []string {
	t.Helper()
	fx := newFixture(t, in, msgs)
	var ops []string
	f := &faultFS{inner: newRootFS(fx.root), hook: func(op, name string) error {
		ops = append(ops, op+" "+name)
		return nil
	}}
	res, _ := fx.write(context.Background(), f)
	if res.Outcome != OutDone {
		t.Fatalf("the clean run: %s %v", res.Outcome, res.Err)
	}
	return ops
}

// writeCrashing runs a write that crashes (a panic, as a power cut) at the k-th
// operation; it returns the restore's ID and whether the crash came.
func (fx *fixture) writeCrashing(k int) (string, bool) {
	fx.t.Helper()
	n := 0
	f := &faultFS{inner: newRootFS(fx.root), hook: func(op, name string) error {
		n++
		if n == k {
			panic(crash{})
		}
		return nil
	}}
	id, err := NewID()
	if err != nil {
		fx.t.Fatal(err)
	}
	crashed := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(crash); !ok {
					panic(r)
				}
				crashed = true
			}
		}()
		Write(context.Background(), f, fx.input(id))
	}()
	return id, crashed
}

// madeOnDisk are the entries in the container now (relative, "/"-joined), the marker
// and the made-list aside.
func madeOnDisk(t *testing.T, container string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(container, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == container {
			return nil
		}
		rel, _ := filepath.Rel(container, p)
		rel = filepath.ToSlash(rel)
		if rel == MarkerName || rel == MadeListName {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	return out
}

// listed are the made-list's paths.
func listed(t *testing.T, root *os.Root, container string) map[string]bool {
	t.Helper()
	l, err := readMadeList(newRootFS(root), container)
	if err != nil {
		t.Fatalf("made-list: %v", err)
	}
	out := map[string]bool{}
	for _, e := range l {
		out[e.P] = true
	}
	return out
}

func mustContain(t *testing.T, s string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if !strings.Contains(s, n) {
			t.Fatalf("want %q in %q", n, s)
		}
	}
}

func dump(ev []string) string { return fmt.Sprint(ev) }

func lastEv(ev []string) string {
	if len(ev) == 0 {
		return ""
	}
	return ev[len(ev)-1]
}

// keptAll reports every line of the before snapshot still in after.
func keptAll(before, after string) bool {
	have := map[string]bool{}
	for _, l := range strings.Split(after, "\n") {
		have[l] = true
	}
	for _, l := range strings.Split(before, "\n") {
		if !have[l] {
			return false
		}
	}
	return true
}

// errFrom is a typed W-KEPT refusal when rc is 1 (the state a test asserts was
// reached), else a plain error with the words: so assure.Refused checks the words and
// that nothing changed.
func errFrom(rc int, words string) error {
	if rc == 1 {
		return &Refusal{Code: "W-KEPT", Words: words}
	}
	return errors.New(words)
}

// stepOf is the 1-based step of a clean run at which op is about to run (a crash
// there comes just before it).
func stepOf(t *testing.T, in []FolderIn, msgs [][][]byte, op string) int {
	t.Helper()
	for i, o := range opsOf(t, in, msgs) {
		if o == op {
			return i + 1
		}
	}
	t.Fatalf("no step %q", op)
	return 0
}

func mustRead(t *testing.T, l *Log) []LogLine {
	t.Helper()
	lines, err := l.Read()
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

func fileExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func syscallENOSPC() error { return &os.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC} }
