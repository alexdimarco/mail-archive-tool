package index

import (
	"errors"
	"os"
	"testing"
	"time"
)

// covers: MA-757, R37
// An encrypted archive's search index lives only in memory (Phase 2b-3, design
// phase2b §6, A23): a new one opens with no image; rows added to it are found; its
// captured image is a rollback-journal database that loads back into memory with
// every row, searchable — and nothing is written to the working folder.
func TestAnInMemoryIndexRoundTripsThroughItsImage(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	ix, err := OpenMemory(nil)
	if err != nil {
		t.Fatal(err)
	}
	jul := time.Date(2025, 7, 3, 9, 0, 0, 0, time.UTC)
	if err := ix.Add("s", []string{"Inbox"}, mkMsg("Acme invoice #4471", "bob", "me", "<p>the revised invoice</p>", jul, "invoice.pdf"), "Inbox/x.html", "s\x00a"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Add("s", []string{"Inbox"}, mkMsg("Lunch?", "carol", "me", "free tomorrow?", jul), "Inbox/y.html", "s\x00b"); err != nil {
		t.Fatal(err)
	}
	img, err := ix.Image()
	if err != nil {
		t.Fatal(err)
	}
	ix.Close()
	if len(img) < 100 || img[18] != 1 || img[19] != 1 {
		t.Fatalf("the image is not a rollback-journal database (%d bytes)", len(img))
	}
	again, err := OpenMemory(img)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	for i := range img {
		img[i] = 0 // the loaded index is a copy, not a view of the caller's bytes
	}
	if n, err := again.Count(); err != nil || n != 2 {
		t.Fatalf("the reloaded index holds %d rows (%v), want 2", n, err)
	}
	res, total, err := again.Search(Query{Text: "acme invoice"})
	if err != nil || total != 1 || res[0].Subject != "Acme invoice #4471" {
		t.Fatalf("search after reload: %d %+v %v", total, res, err)
	}
	if entries, _ := os.ReadDir(cwd); len(entries) != 0 {
		t.Fatalf("an in-memory index wrote %d files to the working folder", len(entries))
	}
}

// covers: MA-757, R37
// The in-memory index keeps the rollback journal Add's per-row savepoints need
// (journal_mode=MEMORY, never OFF — the spike showed OFF corrupts FTS5 on ROLLBACK
// TO): a row refused mid-batch is rolled back alone, the batch keeps the others, and
// the full-text table stays consistent.
func TestTheInMemoryIndexRollsBackOneRowAlone(t *testing.T) {
	ix, err := OpenMemory(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	var mode string
	if err := ix.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "memory" {
		t.Fatalf("journal mode %q (%v), want memory", mode, err)
	}
	if _, err := ix.db.Exec(`CREATE TEMP TRIGGER refuse BEFORE INSERT ON docs WHEN NEW.key = 's' || char(0) || 'bad' BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2025, 7, 3, 9, 0, 0, 0, time.UTC)
	ix.Add("s", []string{"Inbox"}, mkMsg("first", "a", "me", "one", day), "x/1.html", "s\x00one")
	if err := ix.Add("s", []string{"Inbox"}, mkMsg("refused", "a", "me", "two", day), "x/2.html", "s\x00bad"); err == nil {
		t.Fatal("fixture: the trigger did not refuse the row")
	}
	ix.Add("s", []string{"Inbox"}, mkMsg("third", "a", "me", "three", day), "x/3.html", "s\x00three")
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}
	var docs, fts int
	ix.db.QueryRow(`SELECT count(*) FROM docs`).Scan(&docs)
	ix.db.QueryRow(`SELECT count(*) FROM docs_fts`).Scan(&fts)
	if docs != 2 || fts != 2 {
		t.Fatalf("docs %d, fts %d: want the two rows on either side of the refused one", docs, fts)
	}
	if _, err := ix.db.Exec(`INSERT INTO docs_fts(docs_fts) VALUES('integrity-check')`); err != nil {
		t.Fatalf("the full-text index is damaged after a rollback: %v", err)
	}
}

// covers: MA-757, R37
// An image that is not a database this build seals is refused (ErrImage): bytes
// that are not SQLite, and a database with a write-ahead-log header (which would
// not load from bytes); one from a newer MailArchive asks for an upgrade
// (ErrNewerFormat).
func TestAnInMemoryIndexRefusesAForeignImage(t *testing.T) {
	ix, _ := OpenMemory(nil)
	img, _ := ix.Image()
	ix.Close()
	wal := append([]byte(nil), img...)
	wal[18], wal[19] = 2, 2
	for label, b := range map[string][]byte{"not a database": []byte("hello, this is not sqlite at all, not even close, no header here at all ............................"), "a WAL header": wal} {
		if _, err := OpenMemory(b); !errors.Is(err, ErrImage) {
			t.Errorf("%s: want ErrImage, got %v", label, err)
		}
	}
	newer, _ := OpenMemory(nil)
	newer.db.Exec(`UPDATE meta SET version = ?`, indexVersion+1)
	nb, _ := newer.Image()
	newer.Close()
	if _, err := OpenMemory(nb); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("a newer index image: want ErrNewerFormat, got %v", err)
	}
}
