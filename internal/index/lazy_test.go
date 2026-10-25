package index

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// lazyImage is a two-row index's image, and a loader of it that counts its calls.
func lazyImage(t *testing.T) (img []byte, load func() ([]byte, error), calls *int) {
	t.Helper()
	ix, err := OpenMemory(nil)
	if err != nil {
		t.Fatal(err)
	}
	jul := time.Date(2025, 7, 3, 9, 0, 0, 0, time.UTC)
	ix.Add("s", []string{"Inbox"}, mkMsg("Acme invoice #4471", "bob", "me", "the revised invoice", jul), "Inbox/x.html", "s\x00a")
	ix.Add("s", []string{"Inbox"}, mkMsg("Lunch?", "carol", "me", "free tomorrow?", jul), "Inbox/y.html", "s\x00b")
	if img, err = ix.Image(); err != nil {
		t.Fatal(err)
	}
	ix.Close()
	calls = new(int)
	return img, func() ([]byte, error) { *calls++; return img, nil }, calls
}

// covers: MA-758, R37
// A lazily opened index (design phase2b §6, P2b-C15) answers from its record what a
// run with nothing new asks — its row count, that it changed nothing, that its keys
// need no repair — and closes, all without loading; the first real use loads it once,
// and its change count starts from that load.
func TestALazyIndexLoadsOnlyOnFirstUse(t *testing.T) {
	_, load, calls := lazyImage(t)
	ix, err := OpenLazy(load, 2, indexVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := ix.Count(); err != nil || n != 2 {
		t.Fatalf("an unloaded index counts %d (%v), want the record's 2", n, err)
	}
	if n, err := ix.Changes(); err != nil || n != 0 {
		t.Fatalf("an unloaded index reports %d changes (%v), want 0", n, err)
	}
	if _, err := ix.RepairKeys(false, func(k, p string) (string, bool) { return k, false }, log.Default()); err != nil {
		t.Fatal(err)
	}
	if err := ix.Rekey(nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 {
		t.Fatalf("the index was loaded %d time(s) before its first real use", *calls)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}

	ix, _ = OpenLazy(load, 2, indexVersion, nil)
	defer ix.Close()
	res, total, err := ix.Search(Query{Text: "acme invoice"})
	if err != nil || total != 1 || res[0].Subject != "Acme invoice #4471" {
		t.Fatalf("search on first use: %d %+v %v", total, res, err)
	}
	if n, err := ix.Changes(); err != nil || n != 0 {
		t.Fatalf("loading the index counted %d changes (%v), want 0", n, err)
	}
	if err := ix.UpdateFolder("s\x00a", "Archive"); err != nil {
		t.Fatal(err)
	}
	if n, _ := ix.Changes(); n != 1 {
		t.Fatalf("one write after the load counts %d changes, want 1", n)
	}
	if n, _ := ix.Count(); n != 2 || *calls != 1 {
		t.Fatalf("a loaded index counts %d rows after %d load(s), want 2 after 1", n, *calls)
	}
}

// covers: MA-759, R37
// An index above the memory budget is worked on in a scratch file (design phase2b
// §6, P2b-C15): loaded on first use by streaming the image into it, readable only by
// its owner, searchable and writable there with nothing beside it (no journal file);
// its capture is the file itself and loads back with every row; a file already at
// the path is replaced; and closing the index removes it.
func TestAScratchIndexIsWorkedOnInAFileAndRemovedOnClose(t *testing.T) {
	img, _, _ := lazyImage(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "search.db")
	if err := os.WriteFile(path, []byte("left by a crashed run"), 0o600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	ix, err := OpenScratch(path, func(w io.Writer) error { writes++; _, err := w.Write(img); return err }, 2, indexVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := ix.Count(); n != 2 || writes != 0 {
		t.Fatalf("an unloaded scratch index counts %d after %d write(s), want 2 after none", n, writes)
	}
	res, total, err := ix.Search(Query{Text: "acme invoice"})
	if err != nil || total != 1 || res[0].Subject != "Acme invoice #4471" {
		t.Fatalf("search in the scratch file: %d %+v %v", total, res, err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the scratch file must exist, readable only by its owner: %v %v", fi, err)
	}
	jul := time.Date(2025, 7, 3, 9, 0, 0, 0, time.UTC)
	if err := ix.Add("s", []string{"Inbox"}, mkMsg("Third", "dan", "me", "a third", jul), "Inbox/z.html", "s\x00c"); err != nil {
		t.Fatal(err)
	}
	r, size, err := ix.Capture()
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, size)
	if _, err := r.ReadAt(got, 0); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("the scratch folder holds %d files, want only the index", len(entries))
	}
	again, err := OpenMemory(got)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := again.Count(); n != 3 {
		t.Fatalf("the captured scratch index holds %d rows, want 3", n)
	}
	again.Close()
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("closing the index must remove its scratch file (%v)", err)
	}
}

// covers: MA-762, R37
// A scratch index whose folder's path holds a '?' is worked on in its own file: the
// path is never cut at the '?' into a file outside the scratch folder.
func TestAScratchIndexUnderAPathWithAQuestionMarkStaysInItsFolder(t *testing.T) {
	img, _, _ := lazyImage(t)
	root := t.TempDir()
	dir := filepath.Join(root, "we?ird")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "search.db")
	ix, err := OpenScratch(path, func(w io.Writer) error { _, err := w.Write(img); return err }, 2, indexVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	jul := time.Date(2025, 7, 3, 9, 0, 0, 0, time.UTC)
	if err := ix.Add("s", []string{"Inbox"}, mkMsg("Third", "dan", "me", "a third", jul), "Inbox/z.html", "s\x00c"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 1 || entries[0].Name() != "we?ird" {
		t.Fatalf("the index was worked on outside its folder: %v", entries)
	}
	r, size, err := ix.Capture()
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, size)
	if _, err := r.ReadAt(got, 0); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	again, err := OpenMemory(got)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if n, _ := again.Count(); n != 3 {
		t.Fatalf("the captured scratch file holds %d rows, want the 3 written", n)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-761, R37
// An in-memory index that outgrew the memory budget during a run moves to a scratch
// file: its rows, its search and its change count carry on there, the file is
// readable only by its owner, its capture is the file, and closing removes it. An
// index loaded in neither way cannot move.
func TestAnInMemoryIndexMovesToAScratchFile(t *testing.T) {
	_, load, _ := lazyImage(t)
	ix, err := OpenLazy(load, 2, indexVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := ix.MemorySize(); n != 0 {
		t.Fatalf("an unloaded index takes %d bytes, want 0", n)
	}
	if err := ix.UpdateFolder("s\x00a", "Archive"); err != nil {
		t.Fatal(err)
	}
	if n, err := ix.MemorySize(); err != nil || n <= 0 {
		t.Fatalf("a loaded in-memory index takes %d bytes (%v)", n, err)
	}
	path := filepath.Join(t.TempDir(), "search.db")
	if err := ix.MoveToScratch(path); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the moved index must be a scratch file readable only by its owner: %v %v", fi, err)
	}
	if n, _ := ix.Changes(); n != 1 {
		t.Fatalf("the move changed the change count to %d, want the 1 written before it", n)
	}
	if n, _ := ix.MemorySize(); n != 0 {
		t.Fatalf("a moved index still takes %d bytes of memory", n)
	}
	jul := time.Date(2025, 7, 3, 9, 0, 0, 0, time.UTC)
	if err := ix.Add("s", []string{"Inbox"}, mkMsg("Third", "dan", "me", "a third", jul), "Inbox/z.html", "s\x00c"); err != nil {
		t.Fatal(err)
	}
	if res, total, err := ix.Search(Query{Text: "acme invoice"}); err != nil || total != 1 || res[0].Folder != "Archive" {
		t.Fatalf("search after the move: %d %+v %v", total, res, err)
	}
	r, size, err := ix.Capture()
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := r.(*os.File); !ok || f.Name() != path {
		t.Fatalf("the moved index's capture must be its scratch file, got %T", r)
	}
	got := make([]byte, size)
	if _, err := r.ReadAt(got, 0); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	again, err := OpenMemory(got)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := again.Count(); n != 3 {
		t.Fatalf("the moved index's capture holds %d rows, want 3", n)
	}
	again.Close()
	if err := ix.MoveToScratch(path + "2"); err == nil {
		t.Fatal("an index already in a scratch file moved again")
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("closing the moved index must remove its scratch file (%v)", err)
	}
}

// covers: MA-759, R37
// A scratch image that cannot be loaded — its stream fails part-way, or it is a
// write-ahead-log database, which would make SQLite create companion files — is
// reported and replaced by an empty index in a fresh file; a new scratch index (no
// image) starts empty without a report.
func TestAScratchIndexThatCannotLoadIsReplacedByAnEmptyOne(t *testing.T) {
	img, _, _ := lazyImage(t)
	wal := append([]byte(nil), img...)
	wal[18], wal[19] = 2, 2
	for name, write := range map[string]func(io.Writer) error{
		"stream fails": func(w io.Writer) error { w.Write(img[:4096]); return errors.New("a damaged chunk") },
		"wal image":    func(w io.Writer) error { _, err := w.Write(wal); return err },
		"new index":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "search.db")
			var bad []error
			ix, err := OpenScratch(path, write, 2, indexVersion, func(err error) { bad = append(bad, err) })
			if err != nil {
				t.Fatal(err)
			}
			defer ix.Close()
			if n, err := ix.Folders(); err != nil || len(n) != 0 {
				t.Fatalf("the replacement index has folders %v (%v), want none", n, err)
			}
			if want := map[bool]int{true: 0, false: 1}[write == nil]; len(bad) != want {
				t.Fatalf("the load was reported %d time(s), want %d", len(bad), want)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 1 {
				t.Fatalf("the scratch folder holds %d files, want only the index", len(entries))
			}
		})
	}
}

// covers: MA-758, R37
// A record that says its index is older than this build's key format has it loaded
// for the repair; one a newer MailArchive wrote is refused at open, never loaded.
func TestALazyIndexIsRepairedOrRefusedByItsRecordedVersion(t *testing.T) {
	_, load, calls := lazyImage(t)
	ix, err := OpenLazy(load, 2, indexVersion-1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if _, err := ix.RepairKeys(false, func(k, p string) (string, bool) { return k, false }, log.Default()); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 || ix.Version() != indexVersion {
		t.Fatalf("an older recorded version: %d load(s), version %d after the repair; want 1 and %d", *calls, ix.Version(), indexVersion)
	}
	if _, err := OpenLazy(load, 2, indexVersion+1, nil); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("a newer recorded version: want ErrNewerFormat, got %v", err)
	}
	if *calls != 1 {
		t.Fatal("a newer recorded version was loaded")
	}
}

// covers: MA-758, R37
// An image that cannot be loaded — the record's chunks damaged, or bytes that are not
// a database — is reported and replaced by an empty index, so the backup goes on
// (P2b-C16).
func TestALazyIndexThatCannotLoadIsReplacedByAnEmptyOne(t *testing.T) {
	for name, load := range map[string]func() ([]byte, error){
		"unreadable": func() ([]byte, error) { return nil, errors.New("a damaged chunk") },
		"not a db":   func() ([]byte, error) { return make([]byte, 4096), nil },
	} {
		t.Run(name, func(t *testing.T) {
			var bad []error
			ix, err := OpenLazy(load, 2, indexVersion, func(err error) { bad = append(bad, err) })
			if err != nil {
				t.Fatal(err)
			}
			defer ix.Close()
			if n, err := ix.Folders(); err != nil || len(n) != 0 {
				t.Fatalf("the replacement index has folders %v (%v), want none", n, err)
			}
			if len(bad) != 1 {
				t.Fatalf("the failed load was reported %d time(s), want once", len(bad))
			}
			if n, _ := ix.Count(); n != 0 {
				t.Fatalf("the replacement index counts %d rows, want 0", n)
			}
			// Making the replacement (its schema and version row) is the load, not a
			// change: the run's change count starts after it.
			if n, err := ix.Changes(); err != nil || n != 0 {
				t.Fatalf("the replacement index reports %d changes (%v), want 0", n, err)
			}
		})
	}
}
