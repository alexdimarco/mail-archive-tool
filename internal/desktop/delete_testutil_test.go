package desktop

import (
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/schedule"
)

// Helpers for the Desktop's delete routes (docs/design-archive-delete.md §3, slice 3): a
// Desktop with no -out, its list of archives beside its settings, and a recording stand-in for
// the computer's scheduler; real archives in temp folders (no mock of the removal).

// delSched is the computer's scheduler as the delete reaches it (archdelete.Scheduler): it
// holds entries by name and records each removal; failOn makes a removal fail.
type delSched struct {
	mu        sync.Mutex
	installed map[string]bool
	removed   []string
	failOn    map[string]error
	// onRemove, when set, runs at each removal before it is made: a test stands inside a delete.
	onRemove func(name string)
}

func newDelSched() *delSched {
	return &delSched{installed: map[string]bool{}, failOn: map[string]error{}}
}

func (f *delSched) Query(n string) schedule.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.installed[n] {
		return schedule.Installed
	}
	return schedule.NotInstalled
}
func (f *delSched) SaysMissing(string) bool      { return true }
func (f *delSched) SideFilesExist(string) bool   { return false }
func (f *delSched) RemoveSideFiles(string) error { return nil }
func (f *delSched) RemoveLiveEntry(n string) error {
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

func (f *delSched) removedNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.removed...)
}

// delEnv is a Desktop started without -out, whose list of archives is beside its settings.
type delEnv struct {
	*wsEnv
	sched *delSched
}

func newDeleteEnv(t *testing.T, opts ...wsOpt) *delEnv {
	t.Helper()
	sched := newDelSched()
	all := append([]wsOpt{func(e *wsEnv) {
		e.cfg.Out = ""
		e.cfg.IndexPath = filepath.Join(e.dir, archindex.Name)
		e.cfg.DeleteScheduler = sched
		e.cfg.DeleteInstalled = func() []schedule.InstalledEntry { return nil }
	}}, opts...)
	return &delEnv{wsEnv: newWsEnv(t, all...), sched: sched}
}

// list adds folders to Your archives as rows. The Desktop's reconcile adds only a folder that
// holds an archive; a row whose folder was unplugged, or emptied, since stays, which is what
// adding it directly stands for here.
func (e *delEnv) list(paths ...string) {
	e.t.Helper()
	idx, err := archindex.ReadFile(e.cfg.IndexPath)
	if err != nil {
		e.t.Fatal(err)
	}
	now := time.Now()
	for _, p := range paths {
		dup := false
		for _, en := range idx.Entries {
			dup = dup || en.Path == p
		}
		if !dup {
			idx.Entries = append(idx.Entries, archindex.Entry{Path: p, AddedAt: now, LastSeenAt: now})
		}
	}
	if err := archindex.WriteFile(e.cfg.IndexPath, idx); err != nil {
		e.t.Fatal(err)
	}
}

// rows are the paths Your archives lists.
func (e *delEnv) rows() []string {
	e.t.Helper()
	idx, err := archindex.ReadFile(e.cfg.IndexPath)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, en := range idx.Entries {
		out = append(out, en.Path)
	}
	return out
}

func (e *delEnv) listed(p string) bool {
	for _, r := range e.rows() {
		if r == p {
			return true
		}
	}
	return false
}

// plainArchive makes a real plain archive (the exporter's own files) in a folder named name,
// beside the person's own file, and lists it.
func (e *delEnv) plainArchive(name string) string {
	e.t.Helper()
	dir := filepath.Join(e.t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	buildArchiveAt(e.t, dir)
	e.list(dir)
	return dir
}

// plan posts the plan route for path.
func (e *delEnv) plan(path string) map[string]any {
	e.t.Helper()
	_, m := e.post("/api/archive/delete/plan", map[string]any{"path": path})
	return m
}

// deleteWith posts the delete route.
func (e *delEnv) deleteWith(token, typed string, unrecorded bool) map[string]any {
	e.t.Helper()
	_, m := e.post("/api/archive/delete", map[string]any{"token": token, "typed": typed, "unrecorded": unrecorded})
	return m
}

// planAndDelete plans path and deletes it with its own name typed; it requires both to answer.
func (e *delEnv) planAndDelete(path string) map[string]any {
	e.t.Helper()
	p := e.plan(path)
	if p["ok"] != true {
		e.t.Fatalf("the plan of %s: %v", path, p)
	}
	return e.deleteWith(p["token"].(string), p["name"].(string), false)
}

// page is the Overview as a browser loads it.
func (e *delEnv) page() string {
	e.t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:8097"
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		e.t.Fatalf("the Overview: %d", rec.Code)
	}
	return rec.Body.String()
}

var heldAttrRe = regexp.MustCompile(`data-held-deletes="([^"]*)"`)

// heldOnPage are the held delete results the Overview carries.
func (e *delEnv) heldOnPage() []map[string]any {
	e.t.Helper()
	m := heldAttrRe.FindStringSubmatch(e.page())
	if m == nil {
		e.t.Fatal("the Overview carries no data-held-deletes")
	}
	var out []map[string]any
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &out); err != nil {
		e.t.Fatalf("data-held-deletes is not JSON: %v: %q", err, m[1])
	}
	return out
}

// replyErr is a reply's refusal, "" when it has none (so a reply that was not refused fails
// the refusal's assertions instead of stopping the test).
func replyErr(m map[string]any) string {
	s, _ := m["error"].(string)
	return s
}

func recordBytes(t *testing.T, m map[string]any) []byte {
	t.Helper()
	s, _ := m["recordBase64"].(string)
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) == 0 {
		t.Fatalf("no record bytes in %v (%v)", m, err)
	}
	return b
}

func deletionsLog(t *testing.T) string {
	t.Helper()
	p, err := archdelete.DefaultLogPath()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	return string(b)
}
