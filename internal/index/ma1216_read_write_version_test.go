package index

import (
	"errors"
	"path/filepath"
	"testing"
)

// covers: MA-1216, R45, S82
// The search index's "highest version this build reads" (4) and "version it writes" (3, the key
// format) are two things (docs/design-gmail-dedup.md §7.4, fold check F3 and fold check 2's B3):
// a fresh index is stamped 3, so v1.6.1 and 1.7.x open it; a forced key repair of an index with
// no doc_folders row stamps 3 again; a version-4 index opens through Open, the in-memory open,
// the lazy open and the folder lister, and a version-3 or 4 index needs no key repair; a
// version-5 index is refused by each of the four opens. Version 4 is stamped only when a
// doc_folders table (slice B3's) holds a row.
func TestTheIndexReadsFourAndWritesThree(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "search.db")
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ix.Close()
	if v := rawMetaVersion(t, path); v != 3 {
		t.Fatalf("a fresh index is stamped %d, want 3 (the key format)", v)
	}
	ix, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ix.RepairKeys(true, func(k, p string) (string, bool) { return k, false }, nil); err != nil {
		t.Fatal(err)
	}
	ix.Close()
	if v := rawMetaVersion(t, path); v != 3 {
		t.Fatalf("a forced repair of an index with no doc_folders row stamps %d, want 3", v)
	}

	// Version 4 is read by all four opens, and needs no repair.
	rawExec(t, path, `UPDATE meta SET version = 4`)
	ix, err = Open(path)
	if err != nil {
		t.Fatalf("Open refuses a version-4 index it reads: %v", err)
	}
	if ix.Version() != 4 {
		t.Fatalf("Open reads version %d, want 4", ix.Version())
	}
	repaired := 0
	if _, err := ix.RepairKeys(false, func(k, p string) (string, bool) { repaired++; return k, false }, nil); err != nil || repaired != 0 {
		t.Fatalf("a version-4 index was repaired (%d rows walked, %v)", repaired, err)
	}
	ix.Close()
	if v := rawMetaVersion(t, path); v != 4 {
		t.Fatalf("an unforced open changed the version to %d", v)
	}
	if h, err := OpenForList(path, ListOptions{}); err != nil {
		t.Fatalf("the folder lister refuses a version-4 index: %v", err)
	} else {
		h.Close()
	}
	mem4, _ := OpenMemory(nil)
	mem4.db.Exec(`UPDATE meta SET version = 4`)
	img4, _ := mem4.Image()
	mem4.Close()
	if m, err := OpenMemory(img4); err != nil {
		t.Fatalf("the in-memory open refuses a version-4 image: %v", err)
	} else {
		m.Close()
	}
	if l, err := OpenLazy(func() ([]byte, error) { return img4, nil }, 0, 4, nil); err != nil {
		t.Fatalf("the lazy open refuses version 4: %v", err)
	} else {
		l.Close()
	}

	// Version 5 is refused by each of the four.
	rawExec(t, path, `UPDATE meta SET version = 5`)
	if _, err := Open(path); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("Open on version 5: %v", err)
	}
	var nie *NewerIndexError
	if _, err := OpenForList(path, ListOptions{}); !errors.As(err, &nie) || nie.Understood != 4 {
		t.Fatalf("the folder lister on version 5: %v, want a NewerIndexError naming 4", err)
	}
	mem5, _ := OpenMemory(nil)
	mem5.db.Exec(`UPDATE meta SET version = 5`)
	img5, _ := mem5.Image()
	mem5.Close()
	if _, err := OpenMemory(img5); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("the in-memory open on version 5: %v", err)
	}
	if _, err := OpenLazy(func() ([]byte, error) { return img5, nil }, 0, 5, nil); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("the lazy open on version 5: %v", err)
	}

	// A doc_folders row (slice B3's table) makes a forced repair stamp 4.
	rawExec(t, path, `UPDATE meta SET version = 3`)
	rawExec(t, path, `CREATE TABLE doc_folders(doc INTEGER, folder TEXT)`)
	ix, _ = Open(path)
	ix.RepairKeys(true, func(k, p string) (string, bool) { return k, false }, nil)
	ix.Close()
	if v := rawMetaVersion(t, path); v != 3 {
		t.Fatalf("an empty doc_folders table made the repair stamp %d, want 3", v)
	}
	rawExec(t, path, `INSERT INTO doc_folders VALUES(1, 'Sent')`)
	ix, _ = Open(path)
	ix.RepairKeys(true, func(k, p string) (string, bool) { return k, false }, nil)
	ix.Close()
	if v := rawMetaVersion(t, path); v != 4 {
		t.Fatalf("a doc_folders row: the repair stamped %d, want 4", v)
	}
}
