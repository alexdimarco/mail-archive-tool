package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/schedule"
)

// Helpers for the command line's delete-archive and unlist-archive (docs/design-archive-delete.md
// §3.3, slice 4): a throwaway home holding the Desktop's settings, its list of archives and
// deletions.log; a throwaway folder for the Desktop's instance locks; a recording stand-in for the
// computer's scheduler; and real archives in temp folders (no mock of the removal).

// cliSched is the computer's scheduler as the delete reaches it (archdelete.Scheduler): entries
// by name, each removal recorded; failOn makes a removal fail; onRemove runs before a removal.
type cliSched struct {
	mu        sync.Mutex
	installed map[string]bool
	removed   []string
	failOn    map[string]error
	onRemove  func(name string)
}

func (f *cliSched) Query(n string) schedule.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.installed[n] {
		return schedule.Installed
	}
	return schedule.NotInstalled
}
func (f *cliSched) SaysMissing(string) bool      { return true }
func (f *cliSched) SideFilesExist(string) bool   { return false }
func (f *cliSched) RemoveSideFiles(string) error { return nil }
func (f *cliSched) RemoveLiveEntry(n string) error {
	if f.onRemove != nil {
		f.onRemove(n)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failOn[n]; err != nil {
		return err
	}
	f.removed = append(f.removed, n)
	delete(f.installed, n)
	return nil
}

func (f *cliSched) removedNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.removed...)
}

// cliDel is one test's world: its home, its instance-lock folder and its scheduler.
type cliDel struct {
	t       *testing.T
	home    string
	lockDir string
	sched   *cliSched
}

func newCLIDel(t *testing.T) *cliDel {
	t.Helper()
	home := t.TempDir()
	for k, v := range map[string]string{"HOME": home, "USERPROFILE": home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"), "XDG_CACHE_HOME": filepath.Join(home, ".cache"),
		"XDG_STATE_HOME": filepath.Join(home, ".local", "state"),
		"APPDATA":        filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA": filepath.Join(home, "AppData", "Local")} {
		t.Setenv(k, v)
	}
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	e := &cliDel{t: t, home: home, lockDir: t.TempDir(),
		sched: &cliSched{installed: map[string]bool{}, failOn: map[string]error{}}}
	origDir, origSched, origInst, origStep := desktopLockDir, deleteScheduler, deleteInstalled, beforeDeleteStep
	origOpen, origWatch, origNotify, origExit := openSavedFile, startVerbWatch, notifyDeleteSignals, deleteExit
	origProbe, origAfter := openProbeFile, afterDeleteReturned
	desktopLockDir = func() (string, error) { return e.lockDir, nil }
	deleteScheduler = e.sched
	deleteInstalled = func() []schedule.InstalledEntry { return nil }
	t.Cleanup(func() {
		desktopLockDir, deleteScheduler, deleteInstalled, beforeDeleteStep = origDir, origSched, origInst, origStep
		openSavedFile, startVerbWatch, notifyDeleteSignals, deleteExit = origOpen, origWatch, origNotify, origExit
		openProbeFile, afterDeleteReturned = origProbe, origAfter
	})
	return e
}

// configDir is the Desktop's settings folder in this home (where a Desktop without -config keeps
// its settings and its list of archives).
func (e *cliDel) configDir() string {
	e.t.Helper()
	d, err := graphconfig.DefaultConfigDir()
	if err != nil {
		e.t.Fatal(err)
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		e.t.Fatal(err)
	}
	return d
}

func (e *cliDel) indexPath() string { return filepath.Join(e.configDir(), archindex.Name) }

func (e *cliDel) settingsPath() string { return filepath.Join(e.configDir(), "desktop-settings.json") }

// list adds folders to Your archives as rows.
func (e *cliDel) list(paths ...string) {
	e.t.Helper()
	idx, err := archindex.ReadFile(e.indexPath())
	if err != nil {
		e.t.Fatal(err)
	}
	now := time.Now()
	for _, p := range paths {
		idx.Entries = append(idx.Entries, archindex.Entry{Path: p, AddedAt: now, LastSeenAt: now})
	}
	if err := archindex.WriteFile(e.indexPath(), idx); err != nil {
		e.t.Fatal(err)
	}
}

func (e *cliDel) listed(p string) bool {
	e.t.Helper()
	idx, err := archindex.ReadFile(e.indexPath())
	if err != nil {
		e.t.Fatal(err)
	}
	for _, en := range idx.Entries {
		if en.Path == p {
			return true
		}
	}
	return false
}

// plainArchive makes a real plain archive in a folder named name, beside the person's own file
// notes.txt, and lists it.
func (e *cliDel) plainArchive(name string) string {
	e.t.Helper()
	dir := filepath.Join(e.t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	box := filepath.Join(e.t.TempDir(), "Inbox")
	if err := os.WriteFile(box, []byte(readMbox), 0o600); err != nil {
		e.t.Fatal(err)
	}
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{box}, Out: dir, Index: true}, log.New(io.Discard, "", 0), nil); err != nil {
		e.t.Fatalf("build the archive: %v", err)
	}
	os.Remove(filepath.Join(dir, ".mailarchive.lock"))
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("the person's own notes\n"), 0o644); err != nil {
		e.t.Fatal(err)
	}
	e.list(dir)
	return dir
}

// desktopRunning stands for a MailArchive Desktop running in the logon session ("" is the
// account's own): it holds that session's instance lock until the returned func runs.
func (e *cliDel) desktopRunning(session string) func() {
	e.t.Helper()
	h, err := instance.Acquire(e.lockDir, session)
	if err != nil {
		e.t.Fatalf("hold the Desktop's lock: %v", err)
	}
	var once sync.Once
	release := func() { once.Do(h.Close) }
	e.t.Cleanup(release)
	return release
}

// logText is deletions.log ("" when it is not there).
func (e *cliDel) logText() string {
	e.t.Helper()
	p, err := archdelete.DefaultLogPath()
	if err != nil {
		e.t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	return string(b)
}

// tree is every file under dir with the SHA-256 of its bytes: two equal trees are byte-identical.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			out[p] = "unreadable"
			return nil
		}
		sum := sha256.Sum256(b)
		out[p] = hex.EncodeToString(sum[:])
		return nil
	})
	return out
}

func sameTree(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// recordDigestOK reports whether a record's last line is the SHA-256 of every byte before it,
// and that digest is on a finished line of deletions.log.
func recordDigestOK(rec, logText string) bool {
	body := strings.TrimRight(rec, "\n")
	i := strings.LastIndex(body, "\n")
	if i < 0 || !strings.HasPrefix(body[i+1:], "Record-SHA256: ") {
		return false
	}
	want := strings.TrimPrefix(body[i+1:], "Record-SHA256: ")
	sum := sha256.Sum256([]byte(rec[:i+1]))
	return hex.EncodeToString(sum[:]) == want && strings.Contains(logText, want)
}

// recordIn is the deletion record printed in out, from its Status line to its digest line.
func recordIn(out string) string {
	i := strings.Index(out, "Status: ")
	if i < 0 {
		return ""
	}
	j := strings.Index(out[i:], "Record-SHA256: ")
	if j < 0 {
		return ""
	}
	k := strings.Index(out[i+j:], "\n")
	if k < 0 {
		return out[i:]
	}
	return out[i : i+j+k+1]
}

// prompted reports whether the person was asked anything containing s.
func prompted(a *scripted, s string) bool {
	for _, p := range a.prompts {
		if strings.Contains(p, s) {
			return true
		}
	}
	return false
}

// hooked is a scripted person who, at a prompt containing at, makes something happen first.
type hooked struct {
	*scripted
	at   string
	hook func()
}

func (h *hooked) line(prompt string) (string, error) {
	if h.hook != nil && strings.Contains(prompt, h.at) {
		h.hook()
		h.hook = nil
	}
	return h.scripted.line(prompt)
}

// fakeSignals is a test's own signal watch for the delete verbs: the real watch (newVerbWatch)
// on a channel the test sends on, its exits recorded instead of ending the test, and what it
// says kept. Each verb's watch is remembered, so a test can ask a verb's phase from a seam.
// atExit, when set before the verb runs, looks at the computer at the moment the process would
// have ended.
type fakeSignals struct {
	mu      sync.Mutex
	sigs    chan os.Signal
	exits   chan int
	said    *lockedBuffer
	watches []*verbWatch
	atExit  func()
}

func (e *cliDel) fakeSignals() *fakeSignals {
	e.t.Helper()
	f := &fakeSignals{sigs: make(chan os.Signal, 4), exits: make(chan int, 8), said: &lockedBuffer{}}
	startVerbWatch = func(_, stdout io.Writer, verb string) *verbWatch {
		v := newVerbWatch(f.sigs, f.said, stdout, verb, func(code int) {
			if f.atExit != nil {
				f.atExit()
			}
			f.exits <- code
		})
		f.mu.Lock()
		f.watches = append(f.watches, v)
		f.mu.Unlock()
		return v
	}
	return f
}

// phase is the phase of the verb running now (its watch is the last started).
func (f *fakeSignals) phase() watchPhase {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.watches) == 0 {
		return -1
	}
	return f.watches[len(f.watches)-1].now()
}

// send delivers s and waits until the watch has taken it (its channel drained).
func (f *fakeSignals) send(t *testing.T, s os.Signal) {
	t.Helper()
	f.sigs <- s
	deadline := time.Now().Add(5 * time.Second)
	for len(f.sigs) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("the signal watch did not take the signal")
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // let it act on what it took
}

// exited is the code the watch exited with, or -1 when it did not.
func (f *fakeSignals) exited() int {
	select {
	case c := <-f.exits:
		return c
	default:
		return -1
	}
}

// waitExit is the code the watch exits with, waiting for it; -1 when it does not within 5s.
func (f *fakeSignals) waitExit() int {
	select {
	case c := <-f.exits:
		return c
	case <-time.After(5 * time.Second):
		return -1
	}
}

// lockedBuffer is a buffer two goroutines write to.
type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// storeFolder is the archive's first store folder (a top-level folder that is not MailArchive's).
func storeFolder(t *testing.T, dir string) string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, en := range ents {
		if en.IsDir() && !strings.HasPrefix(en.Name(), ".") && !strings.HasPrefix(en.Name(), "_") {
			names = append(names, en.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatalf("no store folder in %s", dir)
	}
	return names[0]
}

// mailFolder is a folder of the archive holding a message page.
func mailFolder(t *testing.T, dir string) string {
	t.Helper()
	var found string
	errStop := errors.New("found")
	filepath.WalkDir(filepath.Join(dir, storeFolder(t, dir)), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".html") && !strings.HasPrefix(d.Name(), "index") {
			found = filepath.Dir(p)
			return errStop
		}
		return nil
	})
	if found == "" {
		t.Fatalf("no message page in %s", dir)
	}
	return found
}
