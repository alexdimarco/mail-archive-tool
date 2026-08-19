package index

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestFTS5Smoke verifies the modernc.org/sqlite build includes FTS5 and that the
// external-content + trigger + bm25 + snippet pattern we rely on works.
// covers: MA-24
func TestFTS5Smoke(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "smoke.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	schema := `
CREATE TABLE docs(id INTEGER PRIMARY KEY, subject TEXT, body TEXT, date INTEGER);
CREATE VIRTUAL TABLE docs_fts USING fts5(subject, body, content='docs', content_rowid='id', tokenize='unicode61 remove_diacritics 2');
CREATE TRIGGER docs_ai AFTER INSERT ON docs BEGIN
  INSERT INTO docs_fts(rowid, subject, body) VALUES (new.id, new.subject, new.body);
END;
CREATE TRIGGER docs_ad AFTER DELETE ON docs BEGIN
  INSERT INTO docs_fts(docs_fts, rowid, subject, body) VALUES ('delete', old.id, old.subject, old.body);
END;
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema (FTS5 may be missing): %v", err)
	}

	rows := []struct {
		subject, body string
		date          int64
	}{
		{"Acme invoice #4471", "Please find the revised invoice for Acme attached.", 1720000000},
		{"Lunch?", "Are you free for lunch tomorrow?", 1719000000},
		{"Acme contract", "The Acme contract is ready for signature.", 1718000000},
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO docs(subject, body, date) VALUES(?,?,?)`, r.subject, r.body, r.date); err != nil {
			t.Fatal(err)
		}
	}

	// Ranked full-text query with highlighted snippet, joined back to metadata.
	q := `
SELECT d.subject, snippet(docs_fts, 1, '[', ']', '…', 8) AS hl
FROM docs_fts JOIN docs d ON d.id = docs_fts.rowid
WHERE docs_fts MATCH ?
ORDER BY bm25(docs_fts)
LIMIT 10`
	res, err := db.Query(q, `"acme" "invoice"`)
	if err != nil {
		t.Fatalf("match query: %v", err)
	}
	defer res.Close()

	var got []string
	for res.Next() {
		var subject, hl string
		if err := res.Scan(&subject, &hl); err != nil {
			t.Fatal(err)
		}
		got = append(got, subject)
	}
	if err := res.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "Acme invoice #4471" {
		t.Fatalf("expected the invoice email top, got %v", got)
	}

	// Deletion must keep the FTS index consistent (trigger fires).
	if _, err := db.Exec(`DELETE FROM docs WHERE subject = 'Acme invoice #4471'`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM docs_fts WHERE docs_fts MATCH '"invoice"'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 rows after delete, got %d", n)
	}
}
