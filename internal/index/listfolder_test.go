package index

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// listFixture builds a closed index holding store s (Inbox: three messages, Inbox/Clients:
// one) and store t (Inbox: one), and returns its path.
func listFixture(t *testing.T, extra int) string {
	t.Helper()
	path := filepath.Join(tmpDir(t), "search.db")
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	day := func(n int) time.Time { return time.Date(2025, 3, n, 9, 0, 0, 0, time.UTC) }
	add := func(store, folder, subject string, d time.Time, key string, attach ...string) {
		if err := ix.Add(store, []string{folder}, mkMsg(subject, "bob", "me", "body of "+subject, d, attach...), store+"/"+folder+"/"+key+".html", key); err != nil {
			t.Fatal(err)
		}
	}
	add("s", "Inbox", "oldest", day(1), "s\x00Inbox\x00k1")
	add("s", "Inbox", "newest", day(9), "s\x00Inbox\x00k3", "a.pdf")
	add("s", "Inbox", "middle", day(5), "s\x00Inbox\x00k2")
	add("s", "Inbox/Clients", "client", day(7), "s\x00Inbox/Clients\x00k4")
	add("t", "Inbox", "other store", day(8), "t\x00Inbox\x00k5")
	for i := 0; i < extra; i++ {
		add("s", "Bulk", fmt.Sprintf("bulk %03d", i), day(1).Add(time.Duration(i)*time.Minute), fmt.Sprintf("s\x00Bulk\x00b%03d", i))
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		fi, _ := e.Info()
		out = append(out, fmt.Sprintf("%s:%d", e.Name(), fi.Size()))
	}
	return out
}

// covers: MA-435, MA-531, MA-436, R20, R8
// ListFolder is the workspace's message list: one folder of one store, exact (a subfolder's
// messages are not in its parent's), newest first, a page at a time and never more than 100
// rows a page, each row carrying the manifest key. The index is opened read-only and its
// handle is counted, so a request that does not close it is seen; the open writes nothing,
// and with Immutable (another archive folder) it takes no lock and makes no sidecar.
func TestListFolderIsExactPagedAndReadOnly(t *testing.T) {
	path := listFixture(t, 150)
	dir := filepath.Dir(path)
	before := dirEntries(t, dir)
	raw, _ := os.ReadFile(path)

	for _, opt := range []ListOptions{{}, {Immutable: true}} {
		if n := OpenListHandles(); n != 0 {
			t.Fatalf("%d list handles are open before the test", n)
		}
		h, err := OpenForList(path, opt)
		if err != nil {
			t.Fatalf("OpenForList(%+v): %v", opt, err)
		}
		if OpenListHandles() != 1 {
			t.Errorf("an open list handle is not counted: %d", OpenListHandles())
		}
		if h.Version != indexVersion {
			t.Errorf("the index's version = %d, want %d", h.Version, indexVersion)
		}
		rows, total, err := h.ListFolder("s", "Inbox", 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		if total != 3 || len(assure.Reached(t, rows, "Inbox's rows")) != 3 {
			t.Fatalf("Inbox of store s: total %d, %d rows; want its own 3 (not Inbox/Clients, not store t's)", total, len(rows))
		}
		wantSubj := []string{"newest", "middle", "oldest"}
		for i, r := range rows {
			if r.Subject != wantSubj[i] {
				t.Errorf("row %d = %q, want %q (newest first)", i, r.Subject, wantSubj[i])
			}
		}
		if rows[0].Key != "s\x00Inbox\x00k3" || !rows[0].HasAttach || rows[0].SenderEmail != "bob@example.com" || rows[0].Date.Year() != 2025 {
			t.Errorf("the first row: %+v", rows[0])
		}
		if page, tot, _ := h.ListFolder("s", "Inbox", 1, 1); tot != 3 || len(page) != 1 || page[0].Subject != "middle" {
			t.Errorf("offset 1, limit 1: %+v (total %d)", page, tot)
		}
		if past, tot, _ := h.ListFolder("s", "Inbox", 3, 10); tot != 3 || len(past) != 0 {
			t.Errorf("a page past the end: %+v (total %d)", past, tot)
		}
		if bulk, tot, _ := h.ListFolder("s", "Bulk", 0, 100000); tot != 150 || len(bulk) != 100 {
			t.Errorf("a folder of 150 asked for 100,000 rows: %d rows, total %d; want a page of at most 100 and the whole total", len(bulk), tot)
		}
		if none, tot, _ := h.ListFolder("s", "No such folder", 0, 100); tot != 0 || len(none) != 0 {
			t.Errorf("an unknown folder: %+v %d", none, tot)
		}
		var each []string
		if err := h.EachInFolder("s", "Bulk", func(r ListRow) error { each = append(each, r.Key); return nil }); err != nil || len(each) != 150 {
			t.Errorf("EachInFolder: %d keys, %v; want all 150", len(each), err)
		}
		if err := h.Close(); err != nil {
			t.Fatal(err)
		}
		if n := OpenListHandles(); n != 0 {
			t.Errorf("%d list handle(s) still open after Close", n)
		}
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Error("search.db's bytes changed: a list is read-only")
	}
	if got := dirEntries(t, dir); fmt.Sprint(got) != fmt.Sprint(before) {
		// the read-only open of a WAL database may leave an empty -shm beside it; nothing else
		for _, e := range got {
			if e != "search.db-shm:32768" && e != "search.db-wal:0" && !contains(before, e) {
				t.Errorf("the folder gained %s (before: %v, after: %v)", e, before, got)
			}
		}
	}

	// another archive folder: immutable, no sidecar at all, even beside a -wal it cannot read
	imm := listFixture(t, 0)
	os.WriteFile(imm+"-wal", []byte("not a wal"), 0o600)
	before = dirEntries(t, filepath.Dir(imm))
	h, err := OpenForList(imm, ListOptions{Immutable: true})
	if err != nil {
		t.Fatalf("an immutable open beside a -wal: %v", err)
	}
	if _, total, err := h.ListFolder("s", "Inbox", 0, 100); err != nil || total != 3 {
		t.Errorf("an immutable list: total %d, %v", total, err)
	}
	h.Close()
	if got := dirEntries(t, filepath.Dir(imm)); fmt.Sprint(got) != fmt.Sprint(before) {
		t.Errorf("an immutable open changed the folder: %v -> %v", before, got)
	}
}

// covers: MA-531, R8, R20
// The list opens search.db with SQLite's settings for an untrusted file (design §4.3, RR2-C11), for the
// configured archive and for another archive folder alike: no unsafe functions in a schema
// (trusted_schema off), every cell checked (cell_size_check on), no memory map (mmap_size 0), and a
// connection that cannot write (query_only on).
func TestListOpensTheIndexWithTheSettingsForAnUntrustedFile(t *testing.T) {
	path := listFixture(t, 0)
	for _, opt := range []ListOptions{{}, {Immutable: true}} {
		h, err := OpenForList(path, opt)
		if err != nil {
			t.Fatalf("OpenForList(%+v): %v", opt, err)
		}
		for pragma, want := range map[string]int{"trusted_schema": 0, "cell_size_check": 1, "mmap_size": 0, "query_only": 1} {
			var got int
			if err := h.db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil {
				t.Errorf("%+v: PRAGMA %s: %v", opt, pragma, err)
			} else if got != want {
				t.Errorf("%+v: PRAGMA %s = %d, want %d", opt, pragma, got, want)
			}
		}
		h.Close()
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// covers: MA-435, MA-531, R8, R20
// A list refuses an index it cannot trust: one a newer mailarchive wrote (R-p's cause,
// wrapping ErrNewerFormat), a file that is not there, a `docs` that is a view, and a `docs`
// without the columns this build defines, each as a typed refusal before any query; and a
// pre-meta index is version 1, for the caller to map its keys.
func TestListRefusesAnIndexItCannotTrust(t *testing.T) {
	setup := func(t *testing.T, stmts ...string) string {
		path := listFixture(t, 0)
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		for _, s := range stmts {
			if _, err := db.Exec(s); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
		}
		return path
	}
	for _, opt := range []ListOptions{{}, {Immutable: true}} {
		// a newer format
		path := setup(t, `UPDATE meta SET version = 99`)
		if _, err := OpenForList(path, opt); !errors.Is(err, ErrNewerFormat) {
			t.Errorf("%+v: a newer index: %v, want ErrNewerFormat", opt, err)
		}
		// absent
		if _, err := OpenForList(filepath.Join(t.TempDir(), "search.db"), opt); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%+v: a missing index: %v, want fs.ErrNotExist", opt, err)
		}
		// docs is a view
		view := setup(t, `PRAGMA journal_mode=DELETE`, `ALTER TABLE docs RENAME TO docs_real`, `CREATE VIEW docs AS SELECT * FROM docs_real`)
		var ue *ListUnavailableError
		if _, err := OpenForList(view, opt); !errors.As(err, &ue) {
			t.Errorf("%+v: docs is a view: %v, want *ListUnavailableError", opt, err)
		}
		// docs lacks a column
		thin := setup(t, `PRAGMA journal_mode=DELETE`, `DROP TABLE docs`, `CREATE TABLE docs(id INTEGER PRIMARY KEY, key TEXT)`)
		if _, err := OpenForList(thin, opt); !errors.As(err, &ue) {
			t.Errorf("%+v: docs without its columns: %v, want *ListUnavailableError", opt, err)
		}
		// not a database at all
		junk := filepath.Join(t.TempDir(), "search.db")
		os.WriteFile(junk, []byte("this is not a database, and it is not short enough to look like an empty one......................................"), 0o600)
		if _, err := OpenForList(junk, opt); !errors.As(err, &ue) {
			t.Errorf("%+v: a file that is not a database: %v, want *ListUnavailableError", opt, err)
		}
		// pre-meta: version 1
		old := setup(t, `PRAGMA journal_mode=DELETE`, `DROP TABLE meta`)
		h, err := OpenForList(old, opt)
		if err != nil {
			t.Fatalf("%+v: a pre-meta index: %v", opt, err)
		}
		if h.Version != 1 {
			t.Errorf("%+v: a pre-meta index reads as version %d, want 1", opt, h.Version)
		}
		h.Close()
	}
	if n := OpenListHandles(); n != 0 {
		t.Errorf("%d list handle(s) leaked by refused opens", n)
	}
}
