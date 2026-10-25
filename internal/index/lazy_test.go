package index

import (
	"errors"
	"log"
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
