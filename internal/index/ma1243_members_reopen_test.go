package index

import (
	"io"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// covers: MA-1243, R45
// A key's member rows go with every row delete however the index was opened, and move with a
// key repair (docs/design-gmail-dedup.md §8.2 fold). Positive twin first: on a reopened index
// the email is under Sent Mail. Then: a reopened writable index's Add replace takes the rows
// (the open knows the table exists); a forced key repair moves them to the new key; member
// rows whose email row is gone go with a delete of that key; a sealed index opened in memory,
// lazily in memory and lazily in a scratch file each take the rows on Add's replace.
func TestMemberRowsFollowTheKeyOnEveryOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "search.db")
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	membersFixture(t, ix)
	ix.Close()
	mem, err := OpenMemory(nil)
	if err != nil {
		t.Fatal(err)
	}
	membersFixture(t, mem)
	img, err := mem.Image()
	if err != nil {
		t.Fatal(err)
	}
	mem.Close()
	d := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	replace := func(ix *Index) error {
		return ix.Add("s", []string{"All Mail"}, mkMsg("Receipt", "ann", "bob", "again", d), "s/All Mail/Receipt.html", "k-receipt")
	}

	ix, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, n, _ := ix.Search(Query{Folder: "Sent Mail"}); n != 1 {
		t.Fatalf("positive twin: Sent Mail after a reopen finds %d, want 1", n)
	}
	if err := replace(ix); err != nil {
		t.Fatal(err)
	}
	if m, _ := ix.Members("k-receipt"); len(m) != 0 {
		t.Fatalf("a reopened index's Add replace kept members %q", m)
	}

	// A forced key repair moves them.
	ix.SetMembers("k-receipt", "s", []string{"Sent Mail"})
	ix.Flush()
	ix.Close()
	ix, _ = Open(path)
	if _, err := ix.RepairKeys(true, func(k, _ string) (string, bool) { return k + "-2", k == "k-receipt" }, nil); err != nil {
		t.Fatal(err)
	}
	if m, _ := ix.Members("k-receipt-2"); !reflect.DeepEqual(m, []string{"Sent Mail"}) {
		t.Fatalf("the repaired key holds members %q, want [Sent Mail]", m)
	}
	if m, _ := ix.Members("k-receipt"); len(m) != 0 {
		t.Fatalf("the old key still holds members %q after the repair", m)
	}

	// Member rows whose email row is gone go with a delete of that key.
	ix.SetMembers("k-ghost", "s", []string{"Sent Mail"})
	ix.Flush()
	if err := ix.DeleteByKey("k-ghost"); err != nil {
		t.Fatal(err)
	}
	if m, _ := ix.Members("k-ghost"); len(m) != 0 {
		t.Fatalf("a delete of a key with no email row kept its members %q", m)
	}
	ix.Close()

	// The sealed index's three opens.
	for _, c := range []struct {
		name string
		open func() (*Index, error)
	}{
		{"the in-memory open", func() (*Index, error) { return OpenMemory(img) }},
		{"the lazy open", func() (*Index, error) { return OpenLazy(func() ([]byte, error) { return img, nil }, 3, 4, nil) }},
		{"the scratch open", func() (*Index, error) {
			return OpenScratch(filepath.Join(t.TempDir(), "scratch.db"), func(w io.Writer) error { _, err := w.Write(img); return err }, 3, 4, nil)
		}},
	} {
		s, err := c.open()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if err := replace(s); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if m, _ := s.Members("k-receipt"); len(m) != 0 {
			t.Fatalf("%s: Add's replace kept members %q", c.name, m)
		}
		s.Close()
	}
}

// covers: MA-1243, R45
// SetMembers makes the given folders the key's members: setting them again replaces the earlier
// set, never adds to it, and an empty set removes them. A parent folder's filter finds an email
// through a member subfolder (as it finds a subfolder's own emails), and a name that is only a
// prefix does not. A member row naming the email's home folder counts it there once, in the
// filter and the facets. Positive twin first: the first set is read back whole.
func TestSetMembersReplacesTheKeysEarlierMembers(t *testing.T) {
	ix, err := Open(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	membersFixture(t, ix)
	if m, _ := ix.Members("k-receipt"); !reflect.DeepEqual(m, []string{"INBOX", "Sent Mail"}) {
		t.Fatalf("positive twin: members %q, want [INBOX Sent Mail]", m)
	}
	if err := ix.SetMembers("k-receipt", "s", []string{"Important"}); err != nil {
		t.Fatal(err)
	}
	if m, _ := ix.Members("k-receipt"); !reflect.DeepEqual(m, []string{"Important"}) {
		t.Fatalf("after a second set: members %q, want [Important]", m)
	}
	if _, n, _ := ix.Search(Query{Folder: "Sent Mail"}); n != 0 {
		t.Fatalf("Sent Mail still finds %d after the key left it", n)
	}
	// A parent folder's filter takes in its subfolders' members, as it does their own emails.
	if err := ix.SetMembers("k-receipt", "s", []string{"Labels/Work"}); err != nil {
		t.Fatal(err)
	}
	if res, n, _ := ix.Search(Query{Folder: "Labels"}); n != 1 || res[0].Subject != "Receipt" {
		t.Fatalf("the Labels filter finds %d, want Receipt through its member subfolder Labels/Work", n)
	}
	if _, n, _ := ix.Search(Query{Folder: "Label"}); n != 0 {
		t.Fatalf("the Label filter finds %d, want none (a name prefix is not a parent)", n)
	}
	// A member row naming the email's own home folder (load validation drops such a
	// membership; the index does not rely on it) still counts the email once there.
	if err := ix.SetMembers("k-lunch", "s", []string{"INBOX"}); err != nil {
		t.Fatal(err)
	}
	if _, n, _ := ix.Search(Query{Folder: "INBOX"}); n != 1 {
		t.Fatalf("INBOX finds %d, want Lunch once", n)
	}
	fs, _ := ix.Folders()
	for _, f := range fs {
		if f.Folder == "INBOX" && f.Count != 1 {
			t.Fatalf("INBOX's facet counts %d, want Lunch once", f.Count)
		}
	}
	if err := ix.SetMembers("k-receipt", "s", nil); err != nil {
		t.Fatal(err)
	}
	if m, _ := ix.Members("k-receipt"); len(m) != 0 {
		t.Fatalf("after an empty set: members %q, want none", m)
	}
}

// covers: MA-1243, MA-1222, R45
// A reader kept open while a backup writes the first member row sees it (the Desktop's or
// serve's reader opened before the backup): its folder filter and its facets find the email
// under the new folder. Positive twin first: before the write, the reader finds nothing there.
func TestAReaderOpenBeforeTheFirstMemberRowSeesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "search.db")
	w, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	if err := w.Add("s", []string{"INBOX"}, mkMsg("Hi", "ann", "bob", "hello", d), "s/INBOX/Hi.html", "k1"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	for _, c := range []struct {
		name  string
		check func(r *Index) bool
	}{
		{"the folder filter", func(r *Index) bool { _, n, _ := r.Search(Query{Folder: "Sent Mail"}); return n == 1 }},
		{"the facets", func(r *Index) bool {
			fs, _ := r.Folders()
			for _, f := range fs {
				if f.Folder == "Sent Mail" && f.Count == 1 {
					return true
				}
			}
			return false
		}},
	} {
		p := filepath.Join(t.TempDir(), "search.db")
		copyFile(t, path, p)
		r, err := OpenReadonly(p)
		if err != nil {
			t.Fatal(err)
		}
		if c.check(r) {
			t.Fatalf("positive twin: %s finds the email under Sent Mail before any member row", c.name)
		}
		w, err := Open(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.SetMembers("k1", "s", []string{"Sent Mail"}); err != nil {
			t.Fatal(err)
		}
		w.Close()
		if !c.check(r) {
			t.Fatalf("%s of a reader opened before the first member row does not see it", c.name)
		}
		r.Close()
	}
}
