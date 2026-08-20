// Package index builds and queries a SQLite FTS5 full-text index of the
// exported messages, so even very large archives are searchable. The index is a
// single file (search.db) that lives alongside the export and is updated
// incrementally as messages are written.
package index

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"mail-archive-tool/internal/model"
)

const batchSize = 1000

const pragmas = `PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;`

// schema: a metadata table (fast key lookups, filtering, sorting) plus a
// standalone FTS5 table aligned by rowid=docs.id (self-contained, no external
// content quirks). Body text lives only in the FTS table.
const schema = `
CREATE TABLE IF NOT EXISTS docs(
  id           INTEGER PRIMARY KEY,
  key          TEXT UNIQUE NOT NULL,
  store        TEXT,
  folder       TEXT,
  sender_name  TEXT,
  sender_email TEXT,
  recipients   TEXT,
  subject      TEXT,
  date         INTEGER,
  path         TEXT,
  has_attach   INTEGER,
  attach_names TEXT,
  snippet      TEXT
);
CREATE INDEX IF NOT EXISTS idx_docs_date   ON docs(date);
CREATE INDEX IF NOT EXISTS idx_docs_folder ON docs(folder);
CREATE VIRTUAL TABLE IF NOT EXISTS docs_fts USING fts5(
  subject, sender, recipients, folder, attachments, body,
  tokenize='unicode61 remove_diacritics 2'
);`

// bodyColumn is the 0-based index of the "body" column in docs_fts, used by
// snippet().
const bodyColumn = 5

// Index is an open search index.
type Index struct {
	db      *sql.DB
	tx      *sql.Tx
	pending int
}

// Open opens (creating if needed) a writable index at path and begins a batch.
func Open(path string) (*Index, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	db.SetMaxOpenConns(1) // single writer avoids "database is locked"
	if _, err := db.Exec(pragmas); err != nil {
		db.Close()
		return nil, fmt.Errorf("index pragmas: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("index schema: %w", err)
	}
	// Transactions are opened lazily on the first Add so that read methods are
	// never blocked waiting for the single connection an open write tx holds.
	return &Index{db: db}, nil
}

// OpenReadonly opens an existing index for querying (serve/search).
func OpenReadonly(path string) (*Index, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	if _, err := db.Query(`SELECT 1 FROM docs LIMIT 1`); err != nil {
		db.Close()
		return nil, fmt.Errorf("no usable index at %s (run an export first): %w", path, err)
	}
	return &Index{db: db}, nil
}

// Add records (or replaces, by key) one message in the index.
func (ix *Index) Add(store string, folderPath []string, m *model.Message, relPath, key string) error {
	if ix.tx == nil {
		tx, err := ix.db.Begin()
		if err != nil {
			return err
		}
		ix.tx = tx
	}

	folder := strings.Join(folderPath, "/")
	body := BodyText(m)
	snippet := Snippet(body, 240)
	recipients := joinNonEmpty(", ", m.To, m.Cc)
	sender := strings.TrimSpace(strings.TrimSpace(m.SenderName) + " " + strings.TrimSpace(m.SenderEmail))
	attach := attachmentNames(m.Attachments)
	var date int64
	if d := m.Date(); !d.IsZero() {
		date = d.Unix()
	}

	// Replace any existing row with the same key (full-mode re-export).
	if err := deleteByKeyTx(ix.tx, key); err != nil {
		return err
	}

	res, err := ix.tx.Exec(`INSERT INTO docs(key,store,folder,sender_name,sender_email,recipients,subject,date,path,has_attach,attach_names,snippet)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		key, store, folder, m.SenderName, m.SenderEmail, recipients, m.Subject, date, relPath, boolToInt(len(m.Attachments) > 0), attach, snippet)
	if err != nil {
		return fmt.Errorf("index insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := ix.tx.Exec(`INSERT INTO docs_fts(rowid,subject,sender,recipients,folder,attachments,body)
		VALUES(?,?,?,?,?,?,?)`, id, m.Subject, sender, recipients, folder, attach, body); err != nil {
		return fmt.Errorf("index fts insert: %w", err)
	}

	ix.pending++
	if ix.pending >= batchSize {
		return ix.commitPending()
	}
	return nil
}

// commitPending commits the current batch (if any) and releases the connection.
// A new transaction is begun lazily on the next Add.
func (ix *Index) commitPending() error {
	if ix.tx == nil {
		return nil
	}
	err := ix.tx.Commit()
	ix.tx = nil
	ix.pending = 0
	return err
}

// Flush commits everything added so far, making it visible to queries.
func (ix *Index) Flush() error {
	return ix.commitPending()
}

// Close commits any pending batch and closes the index.
func (ix *Index) Close() error {
	if err := ix.commitPending(); err != nil {
		ix.db.Close()
		return err
	}
	return ix.db.Close()
}

func joinNonEmpty(sep string, parts ...string) string {
	var kept []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, sep)
}

func attachmentNames(atts []model.Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	names := make([]string, 0, len(atts))
	for _, a := range atts {
		if n := strings.TrimSpace(a.Filename); n != "" {
			names = append(names, n)
		}
	}
	return strings.Join(names, ", ")
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// deleteByKeyTx removes any existing document (and its FTS row) with the given
// key inside tx. It is the "replace" step of Add, shared with DeleteByKey so the
// two-table delete lives in exactly one place.
func deleteByKeyTx(tx *sql.Tx, key string) error {
	var id int64
	switch err := tx.QueryRow(`SELECT id FROM docs WHERE key=?`, key).Scan(&id); {
	case err == nil:
		return deleteByIDTx(tx, id)
	case errors.Is(err, sql.ErrNoRows):
		return nil
	default:
		return err
	}
}

// deleteByIDTx removes the document with rowid id from both the metadata table
// and the aligned FTS index inside tx.
func deleteByIDTx(tx *sql.Tx, id int64) error {
	if _, err := tx.Exec(`DELETE FROM docs WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM docs_fts WHERE rowid=?`, id); err != nil {
		return err
	}
	return nil
}

// EachRow streams every indexed document's rowid, dedup key, and stored path
// (relative to the export root, forward-slashed) to fn, ordered by rowid. Like
// EachDoc it commits any pending batch first and streams rather than
// materializing. fn MUST NOT write to the index: the single DB connection is
// held by the row cursor for the duration of the walk, so collect the rows you
// want to mutate and delete them after EachRow returns.
func (ix *Index) EachRow(fn func(id int64, key, path string) error) error {
	if err := ix.commitPending(); err != nil {
		return err
	}
	rows, err := ix.db.Query(`SELECT id, key, path FROM docs ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id        int64
			key, path string
		)
		if err := rows.Scan(&id, &key, &path); err != nil {
			return err
		}
		if err := fn(id, key, path); err != nil {
			return err
		}
	}
	return rows.Err()
}

// DeleteByID removes the document with rowid id from both the metadata table and
// the FTS index. Any pending batch is committed first, so it is safe to call
// between EachRow walks.
func (ix *Index) DeleteByID(id int64) error {
	if err := ix.commitPending(); err != nil {
		return err
	}
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	if err := deleteByIDTx(tx, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// DeleteByKey removes the document with the given dedup key (a no-op if absent).
func (ix *Index) DeleteByKey(key string) error {
	if err := ix.commitPending(); err != nil {
		return err
	}
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	if err := deleteByKeyTx(tx, key); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
