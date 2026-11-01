package archdelete

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// fakeSched is a recording host scheduler (§5.3 step 2): entries it knows as
// installed are removed by RemoveLiveEntry; every call is recorded in order.
type fakeSched struct {
	mu          sync.Mutex
	state       map[string]schedule.State
	saysMissing bool
	sideFiles   map[string]bool
	fail        map[string]error
	panicOn     string
	removed     []string
	sideRemoved []string
	onRemove    func(name string)
	events      *[]string
}

func newFakeSched(events *[]string) *fakeSched {
	return &fakeSched{state: map[string]schedule.State{}, sideFiles: map[string]bool{}, fail: map[string]error{}, events: events}
}

func (f *fakeSched) Query(n string) schedule.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state[n]
}
func (f *fakeSched) SaysMissing(string) bool      { return f.saysMissing }
func (f *fakeSched) SideFilesExist(n string) bool { return f.sideFiles[n] }
func (f *fakeSched) RemoveSideFiles(n string) error {
	f.sideRemoved = append(f.sideRemoved, n)
	delete(f.sideFiles, n)
	return nil
}
func (f *fakeSched) RemoveLiveEntry(n string) error {
	if f.onRemove != nil {
		f.onRemove(n)
	}
	if n == f.panicOn {
		panic("the scheduler crashed")
	}
	if err := f.fail[n]; err != nil {
		return err
	}
	f.mu.Lock()
	f.removed = append(f.removed, n)
	f.state[n] = schedule.NotInstalled
	f.mu.Unlock()
	if f.events != nil {
		*f.events = append(*f.events, "schedule:"+n)
	}
	return nil
}

// fakePointers is the settings' pointers.
type fakePointers struct {
	p                Pointers
	loadErr, saveErr error
	saves            int
}

func (f *fakePointers) Load() (Pointers, error) { return f.p, f.loadErr }
func (f *fakePointers) Save(p Pointers) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.p = p
	f.saves++
	return nil
}

// fakeKeys is the Desktop's registry of open encrypted archives.
type fakeKeys struct {
	err    error
	events *[]string
	marked map[string]bool
}

func (f *fakeKeys) LockForDelete(folder string) error {
	*f.events = append(*f.events, "lock-for-delete")
	f.marked[folder] = true
	if f.err == errPanic {
		panic("the registry crashed")
	}
	return f.err
}

// errPanic makes a fake panic instead of failing.
var errPanic = errors.New("panic here")

// testArchiveID is an encrypted archive's id in the tests' record.
const testArchiveID = "0123456789abcdef0123456789abcdef"

// recordedAt is a Record seam saying this computer recorded the archive at the
// folder asked about, under that spelling.
func recordedAt(string) func(string) (RecordRef, error) {
	return func(f string) (RecordRef, error) {
		return RecordRef{Recorded: true, ID: testArchiveID, Spelling: f}, nil
	}
}

// encryptedClean is an encrypted archive holding only its own files: Delete leaves
// nothing of it.
func encryptedClean(t *testing.T) string {
	t.Helper()
	dir := encryptedArchive(t)
	for _, rel := range []string{"units/ab/stray.txt", "inventory/notes.txt"} {
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
func (f *fakeKeys) EndDelete(folder string) {
	*f.events = append(*f.events, "end-delete")
	delete(f.marked, folder)
}

// fakeKeyring is this computer's record of encrypted archives and their keys. Its
// Forget re-creates the archive's lock file when the folder is there, as
// keyring.Forget's lockFolder does.
type fakeKeyring struct {
	plan      KeyPlan
	planErr   error
	forgetErr error
	asked     []string
	forgot    []bool
	folder    string
	onForget  func()
}

func (f *fakeKeyring) PlanForget(spelling string) (KeyPlan, error) {
	f.asked = append(f.asked, spelling)
	return f.plan, f.planErr
}
func (f *fakeKeyring) Forget(p KeyPlan, whole bool) error {
	f.forgot = append(f.forgot, whole)
	if f.onForget != nil {
		f.onForget()
	}
	if f.folder != "" {
		if fi, err := os.Stat(f.folder); err == nil && fi.IsDir() {
			_ = os.WriteFile(filepath.Join(f.folder, lockName), []byte("pid=1\n"), 0o600)
		}
	}
	return f.forgetErr
}

// rig is a delete's seams over real temp folders.
type rig struct {
	dir     string
	x       ExecDeps
	sched   *fakeSched
	ptr     *fakePointers
	keys    *fakeKeys
	kr      *fakeKeyring
	rows    []string
	rowErr  error
	events  []string
	scratch string
}

func newRig(t *testing.T, dir string) *rig {
	t.Helper()
	r := &rig{dir: dir, scratch: t.TempDir()}
	log := filepath.Join(t.TempDir(), LogName)
	deps := testDeps()
	deps.Discover = FileDiscovery{}
	deps.Records = FileDiscovery{Lenient: true}
	deps.Log = log
	r.sched = newFakeSched(&r.events)
	r.sched.state[schedule.DefaultNameFor(dir)] = schedule.Installed
	r.ptr = &fakePointers{p: Pointers{Out: dir, LastArchive: dir}}
	r.keys = &fakeKeys{events: &r.events, marked: map[string]bool{}}
	r.kr = &fakeKeyring{plan: KeyPlan{ID: testArchiveID, Gone: true, Last: true}, folder: dir}
	r.x = ExecDeps{
		Deps: deps, Log: log, Row: dir, Scheduler: r.sched, Pointers: r.ptr,
		Keys: r.keys, Keyring: r.kr, Version: "test",
		ScratchDir: func(spelling, id string) (string, error) { return scratchFor(r.scratch, spelling, id), nil },
		ForgetRow: func(row string) error {
			if r.rowErr != nil {
				return r.rowErr
			}
			r.rows = append(r.rows, row)
			return nil
		},
	}
	return r
}

// scratchFor is the test's stand-in for app.archiveScratch: one folder per spelling
// and archive id.
func scratchFor(root, spelling, id string) string {
	sum := sha256.Sum256([]byte(spelling + "\x00" + id))
	return filepath.Join(root, hex.EncodeToString(sum[:16]))
}

func (r *rig) plan(t *testing.T) *DeletePlan {
	t.Helper()
	return mustPlan(t, r.dir, r.x.Deps)
}

// run plans the folder and deletes it, typing its name.
func (r *rig) run(t *testing.T, unrecorded bool) *Result {
	t.Helper()
	p := r.plan(t)
	return Execute(p, p.Name, unrecorded, r.x)
}

// logLines are deletions.log's lines, each as a JSON object.
func logLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("deletions.log line is not one JSON object: %q", sc.Text())
		}
		out = append(out, m)
	}
	return out
}

// events are the log lines' event names, in order.
func events(lines []map[string]any) []string {
	var out []string
	for _, l := range lines {
		ev, _ := l["event"].(string)
		if o, ok := l["outcome"].(string); ok && o != "" {
			ev += " " + o
		}
		out = append(out, ev)
	}
	return out
}

// appendRawLog appends a line to deletions.log as a crashed run would have left it.
func appendRawLog(t *testing.T, path string, m map[string]any) {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}

// exists reports whether path is there (not followed).
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// hasLine reports whether a result line contains s.
func hasLine(r *Result, s string) bool {
	for _, l := range r.Lines {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

// faultOn makes removeFileFn fail for the names fail picks, recording every name
// asked for, until the test ends.
func faultOn(t *testing.T, seen *[]string, fail func(rel string) error) {
	t.Helper()
	old := removeFileFn
	t.Cleanup(func() { removeFileFn = old })
	removeFileFn = func(root *os.Root, rel string) error {
		if seen != nil {
			*seen = append(*seen, filepath.ToSlash(rel))
		}
		if fail != nil {
			if err := fail(filepath.ToSlash(rel)); err != nil {
				return err
			}
		}
		return old(root, rel)
	}
}
