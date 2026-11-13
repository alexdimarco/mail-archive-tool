package index

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// The Gmail members of the search index (docs/design-gmail-dedup.md §8.2 and §7.4 folds, slice
// B3). One fixture: three emails in store "s". "Receipt" lives in All Mail and is also in INBOX
// and Sent Mail; "Lunch" lives in INBOX; "Plain" lives in Work and is in no other folder.

func membersFixture(t *testing.T, ix *Index) {
	t.Helper()
	d := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	add := func(folder, subject, key string, day int) {
		t.Helper()
		if err := ix.Add("s", strings.Split(folder, "/"), mkMsg(subject, "ann", "bob", subject+" body text", d.AddDate(0, 0, day)), "s/"+folder+"/"+subject+".html", key); err != nil {
			t.Fatal(err)
		}
	}
	add("All Mail", "Receipt", "k-receipt", 0)
	add("INBOX", "Lunch", "k-lunch", 1)
	add("Work", "Plain", "k-plain", 2)
	if err := ix.SetMembers("k-receipt", "s", []string{"Sent Mail", "INBOX", "Sent Mail", ""}); err != nil {
		t.Fatal(err)
	}
	if err := ix.SetMembers("k-plain", "s", nil); err != nil {
		t.Fatal(err)
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}
}

func subjects(rs []Result) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Subject)
	}
	return out
}

// covers: MA-1243, R45
// Positive twin first: an index whose records have no members (an archive nobody switched on)
// gets no doc_folders table, stays version 3 through adds, a SetMembers with no folders, a forced
// repair and a reopen, and so stays one v1.6.1 opens. Then the first member row makes the table
// and stamps version 4 IN THE SAME TRANSACTION: a batch lost after SetMembers takes the table and
// the stamp with it (3, no table), and a committed one leaves 4.
func TestTheFirstMemberRowMakesTheIndexFourInItsTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "search.db")
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	if err := ix.Add("s", []string{"INBOX"}, mkMsg("Hi", "ann", "bob", "hello", d), "s/INBOX/Hi.html", "k1"); err != nil {
		t.Fatal(err)
	}
	if err := ix.SetMembers("k1", "s", nil); err != nil {
		t.Fatal(err)
	}
	ix.Close()
	ix, _ = Open(path)
	ix.RepairKeys(true, func(k, p string) (string, bool) { return k, false }, nil)
	ix.Close()
	if v := rawMetaVersion(t, path); v != 3 {
		t.Fatalf("an index with no members is version %d, want 3", v)
	}
	if hasTable(t, path, membersTable) {
		t.Fatal("an index with no members holds a doc_folders table")
	}

	// A lost batch takes the table and the stamp with it.
	ix, _ = Open(path)
	if err := ix.SetMembers("k1", "s", []string{"Sent Mail"}); err != nil {
		t.Fatal(err)
	}
	if ix.Version() != 4 {
		t.Fatalf("in memory after SetMembers: version %d, want 4", ix.Version())
	}
	_ = ix.abandonBatch(errors.New("lost"))
	if ix.Version() != 3 || ix.members {
		t.Fatalf("after the lost batch: version %d, table %v; want 3 and none", ix.Version(), ix.members)
	}
	ix.Close()
	if v := rawMetaVersion(t, path); v != 3 || hasTable(t, path, membersTable) {
		t.Fatalf("a lost batch left version %d, table %v on disk", v, hasTable(t, path, membersTable))
	}

	// A committed one leaves 4 and the row.
	ix, _ = Open(path)
	if err := ix.SetMembers("k1", "s", []string{"Sent Mail"}); err != nil {
		t.Fatal(err)
	}
	ix.Close()
	if v := rawMetaVersion(t, path); v != 4 {
		t.Fatalf("after the first member row: version %d, want 4", v)
	}
	ix, _ = Open(path)
	defer ix.Close()
	if got, _ := ix.Members("k1"); !reflect.DeepEqual(got, []string{"Sent Mail"}) {
		t.Fatalf("members after reopen: %q", got)
	}
}

// covers: MA-1243, MA-1222, R45
// A folder filter and the facets count each email once: with no folder, one hit per email; the
// Sent Mail filter finds "Receipt", whose home is All Mail; INBOX finds both its own email and
// "Receipt"; a folder's facet counts its home emails and its members, the total counts emails,
// never a sum of folders. A hit names its other folders, sorted.
func TestAFolderFilterAndFacetsCountEachEmailOnce(t *testing.T) {
	ix, err := Open(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	membersFixture(t, ix)

	all, total, err := ix.Search(Query{Sort: "date"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("no filter: %d hits (total %d), want one per email (3)", len(all), total)
	}
	for _, r := range all {
		switch r.Subject {
		case "Receipt":
			if r.Folder != "All Mail" || !reflect.DeepEqual(r.Also, []string{"INBOX", "Sent Mail"}) {
				t.Fatalf("Receipt's hit: home %q, also %q", r.Folder, r.Also)
			}
		default:
			if len(r.Also) != 0 {
				t.Fatalf("%s names other folders: %q", r.Subject, r.Also)
			}
		}
	}
	sent, total, _ := ix.Search(Query{Folder: "Sent Mail"})
	if total != 1 || !reflect.DeepEqual(subjects(sent), []string{"Receipt"}) {
		t.Fatalf("Sent Mail: %q (total %d), want Receipt only", subjects(sent), total)
	}
	inbox, total, _ := ix.Search(Query{Folder: "INBOX", Sort: "date"})
	if total != 2 || !reflect.DeepEqual(subjects(inbox), []string{"Lunch", "Receipt"}) {
		t.Fatalf("INBOX: %q (total %d), want Lunch and Receipt", subjects(inbox), total)
	}
	text, total, _ := ix.Search(Query{Text: "receipt", Folder: "Sent Mail"})
	if total != 1 || len(text) != 1 || !reflect.DeepEqual(text[0].Also, []string{"INBOX", "Sent Mail"}) {
		t.Fatalf("a text search in Sent Mail: %+v (total %d)", text, total)
	}

	facets, err := ix.Folders()
	if err != nil {
		t.Fatal(err)
	}
	want := []FolderCount{{"All Mail", 1}, {"INBOX", 2}, {"Sent Mail", 1}, {"Work", 1}}
	if !reflect.DeepEqual(facets, want) {
		t.Fatalf("facets %+v, want %+v", facets, want)
	}
	if n, _ := ix.Count(); n != 3 {
		t.Fatalf("count %d, want 3 emails", n)
	}
}

// covers: MA-1243, R45
// Every row delete takes the key's member rows in the same transaction: Add's replace (a Full
// re-export), DeleteByID (redaction), DeleteByKey and Rekey's drop; a Rekey rename moves them.
// After each, the filter no longer finds the email under a member folder it lost.
func TestEveryRowDeleteTakesTheKeysMemberRows(t *testing.T) {
	ix, err := Open(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	membersFixture(t, ix)
	d := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

	// Positive twin: before any delete, Sent Mail holds Receipt.
	if _, n, _ := ix.Search(Query{Folder: "Sent Mail"}); n != 1 {
		t.Fatalf("Sent Mail before: %d", n)
	}
	// Add's replace.
	if err := ix.Add("s", []string{"All Mail"}, mkMsg("Receipt", "ann", "bob", "again", d), "s/All Mail/Receipt.html", "k-receipt"); err != nil {
		t.Fatal(err)
	}
	ix.Flush()
	if m, _ := ix.Members("k-receipt"); len(m) != 0 {
		t.Fatalf("Add's replace kept members %q", m)
	}
	if _, n, _ := ix.Search(Query{Folder: "Sent Mail"}); n != 0 {
		t.Fatalf("Sent Mail after the replace: %d", n)
	}

	// DeleteByID (redaction).
	ix.SetMembers("k-receipt", "s", []string{"Sent Mail"})
	ix.Flush()
	var id int64
	ix.EachRow(func(i int64, key, _ string) error {
		if key == "k-receipt" {
			id = i
		}
		return nil
	})
	if err := ix.DeleteByID(id); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, ix, `SELECT count(*) FROM doc_folders WHERE key='k-receipt'`); n != 0 {
		t.Fatalf("DeleteByID left %d member rows", n)
	}

	// DeleteByKey.
	ix.SetMembers("k-lunch", "s", []string{"Sent Mail"})
	ix.Flush()
	if err := ix.DeleteByKey("k-lunch"); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, ix, `SELECT count(*) FROM doc_folders WHERE key='k-lunch'`); n != 0 {
		t.Fatalf("DeleteByKey left %d member rows", n)
	}

	// Rekey: a rename moves them; a drop takes them.
	ix.SetMembers("k-plain", "s", []string{"Sent Mail"})
	ix.Flush()
	if err := ix.Rekey(map[string]string{"k-plain": "k-plain-2"}, nil); err != nil {
		t.Fatal(err)
	}
	if m, _ := ix.Members("k-plain-2"); !reflect.DeepEqual(m, []string{"Sent Mail"}) {
		t.Fatalf("Rekey's rename: members %q", m)
	}
	if err := ix.Rekey(nil, []string{"k-plain-2"}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, ix, `SELECT count(*) FROM doc_folders`); n != 0 {
		t.Fatalf("Rekey's drop left %d member rows", n)
	}
}

// covers: MA-1222, MA-1243, R45
// The folder lister finds a Gmail email under every folder it is in, counted once in each; a
// doc_folders that is a view, or lacks a column, makes the file one a list will not use (after
// the positive twin, a real table, lists).
func TestTheFolderListFindsAnEmailUnderEveryMemberFolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "search.db")
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	membersFixture(t, ix)
	ix.Close()

	h, err := OpenForList(path, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		folder string
		want   []string
	}{{"All Mail", []string{"k-receipt"}}, {"Sent Mail", []string{"k-receipt"}}, {"INBOX", []string{"k-lunch", "k-receipt"}}, {"Work", []string{"k-plain"}}} {
		rows, total, err := h.ListFolder("s", c.folder, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		var keys []string
		for _, r := range rows {
			keys = append(keys, r.Key)
		}
		if total != len(c.want) || !reflect.DeepEqual(keys, c.want) {
			t.Fatalf("%s lists %q (total %d), want %q", c.folder, keys, total, c.want)
		}
		var each []string
		h.EachInFolder("s", c.folder, func(r ListRow) error { each = append(each, r.Key); return nil })
		if !reflect.DeepEqual(each, c.want) {
			t.Fatalf("%s streams %q, want %q", c.folder, each, c.want)
		}
	}
	if rows, total, _ := h.ListFolder("other", "Sent Mail", 0, 10); total != 0 || len(rows) != 0 {
		t.Fatalf("another store's Sent Mail lists %d", total)
	}
	h.Close()

	for _, c := range []struct{ name, ddl, says string }{
		{"a view", `DROP TABLE doc_folders; CREATE VIEW doc_folders AS SELECT key, store, folder FROM docs`, "its doc_folders is a view, not a table"},
		{"no folder column", `DROP TABLE doc_folders; CREATE TABLE doc_folders(key TEXT, store TEXT)`, "its doc_folders table has no folder column"},
	} {
		p := filepath.Join(t.TempDir(), "search.db")
		copyFile(t, path, p)
		rawExec(t, p, c.ddl)
		before := fileBytes(t, p)
		_, err := OpenForList(p, ListOptions{Immutable: true})
		var ue *ListUnavailableError
		if !errors.As(err, &ue) {
			t.Fatalf("%s: %v, want a ListUnavailableError", c.name, err)
		}
		assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(p, c.says), assure.NoSideEffect(func() bool { return string(fileBytes(t, p)) == string(before) }))
	}
}

// covers: MA-1243, R45
// fold check 2's B3: an encrypted archive's index with a member row (version 4) reopens through
// the in-memory open, the lazy open in memory and the lazy open in a scratch file, with its
// members; a version-5 image is refused by each, after that positive twin. The listed open is
// TestTheFolderListFindsAnEmailUnderEveryMemberFolder's.
func TestAnEncryptedIndexWithMembersReopensThroughEveryOpen(t *testing.T) {
	mem, err := OpenMemory(nil)
	if err != nil {
		t.Fatal(err)
	}
	membersFixture(t, mem)
	img, err := mem.Image()
	if err != nil {
		t.Fatal(err)
	}
	if mem.Version() != 4 {
		t.Fatalf("the sealed index's version %d, want 4", mem.Version())
	}
	mem.Close()

	check := func(name string, ix *Index, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s refuses a version-4 index with members: %v", name, err)
		}
		defer ix.Close()
		if _, n, err := ix.Search(Query{Folder: "Sent Mail"}); err != nil || n != 1 {
			t.Fatalf("%s: Sent Mail finds %d (%v), want 1", name, n, err)
		}
		if ix.Version() != 4 {
			t.Fatalf("%s reads version %d", name, ix.Version())
		}
	}
	m, err := OpenMemory(img)
	check("the in-memory open", m, err)
	l, err := OpenLazy(func() ([]byte, error) { return img, nil }, 3, 4, nil)
	check("the lazy open", l, err)
	s, err := OpenScratch(filepath.Join(t.TempDir(), "scratch.db"), func(w io.Writer) error { _, err := w.Write(img); return err }, 3, 4, nil)
	check("the scratch open", s, err)

	five, _ := OpenMemory(img)
	five.db.Exec(`UPDATE meta SET version = 5`)
	img5, _ := five.Image()
	five.Close()
	if _, err := OpenMemory(img5); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("the in-memory open on 5: %v", err)
	}
	if _, err := OpenLazy(func() ([]byte, error) { return img5, nil }, 3, 5, nil); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("the lazy open on 5: %v", err)
	}
	if _, err := OpenScratch(filepath.Join(t.TempDir(), "scratch.db"), nil, 3, 5, nil); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("the scratch open on 5: %v", err)
	}
}

// covers: MA-1222, R45
// EachMember streams each member row with its email's own row, by store and folder: the static
// folder pages' source for an email listed in a folder it does not live in.
func TestEachMemberStreamsTheMemberRows(t *testing.T) {
	ix, err := Open(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	membersFixture(t, ix)
	var got []string
	if err := ix.EachMember(func(r MemberRow) error {
		got = append(got, r.Store+"|"+r.Folder+"|"+r.Path+"|"+r.Subject)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"s|INBOX|s/All Mail/Receipt.html|Receipt", "s|Sent Mail|s/All Mail/Receipt.html|Receipt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("member rows %q, want %q", got, want)
	}
}

func hasTable(t *testing.T, path, name string) bool {
	t.Helper()
	ix, err := OpenReadonly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	return tableExists(ix.db, name)
}

func countRows(t *testing.T, ix *Index, q string) int {
	t.Helper()
	var n int
	if err := ix.db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func fileBytes(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
