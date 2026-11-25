package desktop

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
)

// The stand-alone wizard's made-list as MailArchive Desktop reads it (docs/design-per-mailbox-archives.md
// §10, C10, revision 2.1's F-02; MA-1500). The wizard's record of each folder it makes is step 3;
// here the list is written with the wizard's own writer, job.RecordMade.

// newMadeEnv is a Desktop started without -out whose made-list is in its settings folder.
func newMadeEnv(t *testing.T) *delEnv {
	t.Helper()
	return newDeleteEnv(t, func(e *wsEnv) { e.cfg.MadeList = filepath.Join(e.dir, job.MadeListName) })
}

func record(t *testing.T, file, path string) {
	t.Helper()
	if err := job.RecordMade(file, path, filepath.Base(path)); err != nil {
		t.Fatal(err)
	}
}

// madeNow is the made-list's folders, in order.
func madeNow(t *testing.T, file string) []string {
	t.Helper()
	ms, err := job.ReadMade(file)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range ms {
		out = append(out, m.Path)
	}
	return out
}

// covers: MA-1500
// A page load folds every folder the made-list names into Your archives (with the list's
// Reconcile), so every archive of a set the stand-alone wizard made is listed, not only the one
// last-archive.txt names; then it strikes each line whose folder is now listed and holds an
// archive, or is gone. It keeps a line whose folder is there with no archive yet (F-02: a copy that
// failed before its first save, which the wizard's re-run continues) and lists no row for it, and
// keeps a line whose folder's parent is not there either (a drive unplugged). Once that unfinished
// folder holds an archive, the next page load lists it and strikes its line.
func TestThePageListsTheMadeListsArchivesAndStrikesTheirLines(t *testing.T) {
	e := newMadeEnv(t)
	made := e.cfg.MadeList
	parent := filepath.Join(t.TempDir(), "Mail archives")
	a, b := filepath.Join(parent, "qzsam.walker@gmail.com"), filepath.Join(parent, "Local Folders")
	buildArchiveAt(t, a)
	buildArchiveAt(t, b)
	unfinished := filepath.Join(parent, "sam@contoso.example")
	if err := os.Mkdir(unfinished, 0o755); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(parent, "sam@old.example")
	unplugged := filepath.Join(t.TempDir(), "unplugged drive", "Mail archives", "imap.example.com")
	for _, p := range []string{a, unfinished, gone, b, unplugged} {
		record(t, made, p)
	}

	e.page()
	if !e.listed(a) || !e.listed(b) {
		t.Fatalf("the made-list's archives are not all listed: %v", e.rows())
	}
	for _, p := range []string{unfinished, gone, unplugged} {
		if e.listed(p) {
			t.Errorf("%s, which holds no archive, is listed", p)
		}
	}
	if got, want := fmt.Sprint(madeNow(t, made)), fmt.Sprint([]string{unfinished, unplugged}); got != want {
		t.Fatalf("after the page load the made-list holds %s, want %s", got, want)
	}

	buildArchiveAt(t, unfinished) // the wizard's re-run finished it
	e.page()
	if !e.listed(unfinished) {
		t.Fatalf("the finished folder is not listed: %v", e.rows())
	}
	if got, want := fmt.Sprint(madeNow(t, made)), fmt.Sprint([]string{unplugged}); got != want {
		t.Errorf("after it was listed the made-list holds %s, want %s", got, want)
	}
}

// covers: MA-1500
// A line is struck only against a list of archives that is on disk: while another MailArchive
// program holds Your archives past the wait, the page load cannot write the archive's row, so it
// keeps the line and the next page load lists the archive and strikes it (positive twin).
func TestAMadeLineIsKeptWhenThePageCouldNotWriteTheList(t *testing.T) {
	e := newMadeEnv(t)
	made := e.cfg.MadeList
	a := filepath.Join(t.TempDir(), "Mail archives", "qzsam.walker@gmail.com")
	buildArchiveAt(t, a)
	record(t, made, a)
	held, err := lockfile.Acquire(archindex.LockPath(e.cfg.IndexPath))
	if err != nil {
		t.Fatal(err)
	}
	e.page()
	held.Release()
	if e.listed(a) || fmt.Sprint(madeNow(t, made)) != fmt.Sprint([]string{a}) {
		t.Fatalf("with the list held: listed %v, made-list %v", e.listed(a), madeNow(t, made))
	}
	e.page()
	if !e.listed(a) || len(madeNow(t, made)) != 0 {
		t.Fatalf("the positive twin, the next page load: listed %v, made-list %v", e.listed(a), madeNow(t, made))
	}
}

// covers: MA-1500
// A row the person removes stays removed: Remove from list strikes its folder from the made-list
// before it removes the row, so the next page load does not list it again, though the folder still
// holds its archive. The line is there at Remove from list when the wizard recorded the folder
// after a page load had struck it (or that page load's strike could not be made). Positive twin
// first: the page load listed the archive from the made-list.
func TestARowRemovedFromTheListStaysRemoved(t *testing.T) {
	e := newMadeEnv(t)
	made := e.cfg.MadeList
	a := filepath.Join(t.TempDir(), "Mail archives", "qzsam.walker@gmail.com")
	buildArchiveAt(t, a)
	record(t, made, a)
	e.page()
	if !e.listed(a) || len(madeNow(t, made)) != 0 {
		t.Fatalf("the positive twin: listed %v, made-list %v", e.listed(a), madeNow(t, made))
	}
	record(t, made, a)

	_, q := e.post("/api/archive/unlist", map[string]any{"path": a})
	if _, r := e.post("/api/archive/unlist", map[string]any{"path": a, "confirm": true, "named": q["named"]}); r["ok"] != true || e.listed(a) {
		t.Fatalf("Remove from list: %v (listed %v)", r, e.listed(a))
	}
	if got := madeNow(t, made); len(got) != 0 {
		t.Errorf("Remove from list left the folder in the made-list: %v", got)
	}
	e.page()
	if e.listed(a) {
		t.Fatalf("the page load listed the removed archive again: %v", e.rows())
	}
}

// covers: MA-1500
// Remove from list keeps the row when the made-list's line for its folder cannot be struck
// (another MailArchive program holds the made-list past the wait): the line would list the archive
// again at the next page load. Its reply says the row could not be removed, and the row and the
// line are both still there. Positive twin first: with the made-list free, the same Remove from
// list removes another archive and strikes its line.
func TestRemoveFromListKeepsTheRowWhenItsLineCannotBeStruck(t *testing.T) {
	e := newMadeEnv(t)
	made := e.cfg.MadeList
	parent := filepath.Join(t.TempDir(), "Mail archives")
	a, b := filepath.Join(parent, "a@example.com"), filepath.Join(parent, "b@example.com")
	buildArchiveAt(t, a)
	buildArchiveAt(t, b)
	e.list(a, b)
	record(t, made, a)
	record(t, made, b)
	unlist := func(p string) map[string]any {
		_, q := e.post("/api/archive/unlist", map[string]any{"path": p})
		_, r := e.post("/api/archive/unlist", map[string]any{"path": p, "confirm": true, "named": q["named"]})
		return r
	}
	if r := unlist(b); r["ok"] != true || e.listed(b) || fmt.Sprint(madeNow(t, made)) != fmt.Sprint([]string{a}) {
		t.Fatalf("the positive twin: %v (listed %v, made-list %v)", r, e.listed(b), madeNow(t, made))
	}

	held, err := lockfile.Acquire(job.MadeLockPath(made))
	if err != nil {
		t.Fatal(err)
	}
	r := unlist(a)
	held.Release()
	rc := http.StatusOK
	if r["ok"] == true {
		rc = 0
	}
	assure.Refused(t, rc, replyErr(r), assure.Code(http.StatusOK), assure.Names("Could not remove the row from Your archives"),
		assure.NoSideEffect(func() bool { return e.listed(a) && fmt.Sprint(madeNow(t, made)) == fmt.Sprint([]string{a}) }))
}

// covers: MA-1500
// A delete strikes its folder from the made-list, as a tidy-up that never keeps the row: with the
// made-list free the line goes with the row; with the made-list held past the wait the row is still
// removed and the line stays, and the next page load strikes it, its folder now gone. Positive twin
// first (the made-list free).
func TestADeleteStrikesItsFolderFromTheMadeList(t *testing.T) {
	e := newMadeEnv(t)
	made := e.cfg.MadeList
	a, b := e.plainArchive("Adelete"), e.plainArchive("Bdelete")
	record(t, made, a)
	record(t, made, b)
	if r := e.planAndDelete(a); r["ok"] != true || e.listed(a) || fmt.Sprint(madeNow(t, made)) != fmt.Sprint([]string{b}) {
		t.Fatalf("the positive twin, a delete: %v (listed %v, made-list %v)", r, e.listed(a), madeNow(t, made))
	}

	held, err := lockfile.Acquire(job.MadeLockPath(made))
	if err != nil {
		t.Fatal(err)
	}
	r := e.planAndDelete(b)
	held.Release()
	if _, err := os.Lstat(b); r["ok"] != true || e.listed(b) || !os.IsNotExist(err) {
		t.Fatalf("a delete with the made-list held: %v (listed %v, folder %v)", r, e.listed(b), err)
	}
	if got := madeNow(t, made); fmt.Sprint(got) != fmt.Sprint([]string{b}) {
		t.Fatalf("with the made-list held the line should stay for the page load: %v", got)
	}
	e.page()
	if got := madeNow(t, made); len(got) != 0 || e.listed(b) {
		t.Errorf("the page load kept the deleted folder's line %v (listed %v)", got, e.listed(b))
	}
}

// covers: MA-1500
// The command line's delete verbs read the made-list the Desktop reads (job.MadeListFile): a folder
// it names is a row as the Desktop draws it, and remove-from-list strikes it, so the Desktop does not
// list it again. Positive twin first: the drawn row is found before it is removed.
func TestTheCommandLineDrawsAndStrikesTheMadeList(t *testing.T) {
	c := NewCommandLineDeleteAt(filepath.Join(t.TempDir(), "graph-config.json"), nil, newDelSched(), func() []schedule.InstalledEntry { return nil })
	want, err := job.MadeListFile()
	if err != nil || c.cfg.MadeList != want {
		t.Fatalf("the command line reads the made-list %q, want %q (%v)", c.cfg.MadeList, want, err)
	}
	t.Cleanup(func() {
		os.Remove(want)
		os.Remove(job.MadeLockPath(want))
	})
	a := filepath.Join(t.TempDir(), "Mail archives", "qzsam.walker@gmail.com")
	buildArchiveAt(t, a)
	record(t, want, a)
	row, ok, err := c.Row(a)
	if err != nil || !ok || row != a {
		t.Fatalf("the positive twin: the made-list's archive is a row as drawn: %q %v %v", row, ok, err)
	}
	if err := c.ListRow(row); err != nil {
		t.Fatal(err)
	}
	q, err := archdelete.PlanUnlist(row, c.UnlistDeps(nil))
	if err != nil {
		t.Fatal(err)
	}
	if res := archdelete.Unlist(row, c.UnlistDeps(q.Named)); !res.Removed {
		t.Fatalf("remove-from-list: %v", res.Lines)
	}
	if got := madeNow(t, want); len(got) != 0 {
		t.Errorf("remove-from-list left the folder in the made-list: %v", got)
	}
	if _, ok, _ := c.Row(a); ok {
		t.Error("the removed archive is still a row as the Desktop draws it")
	}
}
