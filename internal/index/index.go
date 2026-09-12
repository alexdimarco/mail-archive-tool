// Package index builds and queries a SQLite FTS5 full-text index of the
// exported messages, so even very large archives are searchable. The index is a
// single file (search.db) that lives alongside the export and is updated
// incrementally as messages are written.
package index

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"strings"

	_ "modernc.org/sqlite"

	"mail-archive-tool/internal/model"
)

const batchSize = 1000

// indexVersion is the schema/key-format version stamped in the meta table. 1 =
// pre-meta (no meta row); 2 = store-scoped keys; 3 = the go-back timeline, where
// a row's key may be a mailbox-wide LiveKey and its folder column follows a move
// (UpdateFolder). An index with no meta row is treated as version 1 and repaired
// by RepairKeys; a pre-go-back version-2 index is re-stamped to 3 on first open
// (its store-scoped keys are already correct, so no row changes); a stored
// version above this is refused by Open (written by a newer mailarchive).
const indexVersion = 3

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
);
CREATE TABLE IF NOT EXISTS meta(version INTEGER);`

// bodyColumn is the 0-based index of the "body" column in docs_fts, used by
// snippet().
const bodyColumn = 5

// Index is an open search index.
type Index struct {
	db      *sql.DB
	tx      *sql.Tx
	pending int
	version int // key-format version read at Open (see indexVersion)
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
	// The version is decided BEFORE the schema runs: a pre-existing docs table
	// with no meta table is a pre-meta index (version 1, needs RepairKeys); a
	// brand-new file is stamped at the current version; a meta row is trusted.
	hadDocs := tableExists(db, "docs")
	hadMeta := tableExists(db, "meta")
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("index schema: %w", err)
	}
	version, err := resolveIndexVersion(db, hadDocs, hadMeta)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("index version: %w", err)
	}
	if version > indexVersion {
		db.Close()
		return nil, fmt.Errorf("search index %s was written by a newer mailarchive (format version %d; this build understands %d): upgrade this copy of mailarchive, or point -out at an archive this version wrote", path, version, indexVersion)
	}
	// Transactions are opened lazily on the first Add so that read methods are
	// never blocked waiting for the single connection an open write tx holds.
	return &Index{db: db, version: version}, nil
}

// tableExists reports whether a table of the given name is present.
func tableExists(db *sql.DB, name string) bool {
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// resolveIndexVersion decides the stored key-format version after the schema
// has been (idempotently) created. A meta row is authoritative; a pre-existing
// docs table with no prior meta table is version 1 (a pre-meta index); a fresh
// database is stamped at the current version.
func resolveIndexVersion(db *sql.DB, hadDocs, hadMeta bool) (int, error) {
	if hadMeta {
		var v sql.NullInt64
		switch err := db.QueryRow(`SELECT version FROM meta LIMIT 1`).Scan(&v); {
		case errors.Is(err, sql.ErrNoRows):
			return 1, nil // meta table present but empty: treat as legacy
		case err != nil:
			return 0, err
		}
		if v.Valid {
			return int(v.Int64), nil
		}
		return 1, nil
	}
	if hadDocs {
		return 1, nil // a pre-meta index carrying one-NUL keys
	}
	if _, err := db.Exec(`INSERT INTO meta(version) VALUES(?)`, indexVersion); err != nil {
		return 0, err
	}
	return indexVersion, nil
}

// OpenReadonly opens an existing index for querying (serve/search).
func OpenReadonly(path string) (*Index, error) {
	// Check for the file first, so a missing index yields a clean "run an export
	// first" rather than the SQLite driver's misleading low-level open error.
	if _, statErr := os.Stat(path); errors.Is(statErr, fs.ErrNotExist) {
		return nil, fmt.Errorf("no search index at %s (run an export first)", path)
	}
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
		return ix.abandonBatch(err)
	}

	res, err := ix.tx.Exec(`INSERT INTO docs(key,store,folder,sender_name,sender_email,recipients,subject,date,path,has_attach,attach_names,snippet)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		key, store, folder, m.SenderName, m.SenderEmail, recipients, m.Subject, date, relPath, boolToInt(len(m.Attachments) > 0), attach, snippet)
	if err != nil {
		return ix.abandonBatch(fmt.Errorf("index insert: %w", err))
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ix.abandonBatch(err)
	}
	if _, err := ix.tx.Exec(`INSERT INTO docs_fts(rowid,subject,sender,recipients,folder,attachments,body)
		VALUES(?,?,?,?,?,?,?)`, id, m.Subject, sender, recipients, folder, attach, body); err != nil {
		// A docs row inserted without its docs_fts twin would leave the two
		// tables misaligned; roll the whole uncommitted batch back so neither a
		// later Flush nor Close can commit the orphan (INT-2).
		return ix.abandonBatch(fmt.Errorf("index fts insert: %w", err))
	}

	ix.pending++
	if ix.pending >= batchSize {
		return ix.commitPending()
	}
	return nil
}

// commitPending commits the current batch (if any) and releases the connection.
// A new transaction is begun lazily on the next Add.
// abandonBatch rolls back the current uncommitted batch and clears it, so a row
// that failed mid-insert can never be committed by a later Flush/Close and the
// next Add begins a fresh transaction. It returns the original error.
func (ix *Index) abandonBatch(cause error) error {
	if ix.tx != nil {
		_ = ix.tx.Rollback()
		ix.tx = nil
		ix.pending = 0
	}
	return cause
}

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

// UpdateFolder rewrites ONLY the folder column of the doc with the given key —
// the body-free index update the mailbox-wide move fast-path issues (§3.2). A
// message that moved between folders keeps its indexed subject, body, path and
// attachment names; only its folder (the browse/facet grouping, `docs.folder`,
// which folder facets and pages read) follows the move, at the cost of one
// UPDATE and no re-tokenising of the body. The `folder` column of the FTS table
// is a full-text search nicety left to the next reindex — `docs.folder` is the
// authoritative browse/facet folder. Any pending batch is committed first, so it
// is safe to call between Add calls during a walk. A no-op when no row has the
// key.
func (ix *Index) UpdateFolder(key, folder string) error {
	if err := ix.commitPending(); err != nil {
		return err
	}
	_, err := ix.db.Exec(`UPDATE docs SET folder=? WHERE key=?`, folder, key)
	return err
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

// RepairKeys re-scopes legacy (v2) index rows to store-qualified (v3) keys,
// applying fn (state.MigrateKey) to each row's (key, path). It runs when force
// is set — the manifest load re-scoped something, the ONLY signal that survives
// an old-binary excursion (a downgrade never regresses the index's own meta
// version) — or when the stored version is below target. It logs "migrating
// index keys (N rows)" before it starts and re-keys every affected row in one
// transaction, then stamps the current version so a later open is a no-op.
// Idempotent: a row already holding a v3 key (fn returns false) is untouched
// and never double-prefixed; when an excursion left a v3 row already occupying
// a re-keyed row's target key, the re-keyed (later) row wins so exactly one row
// per key survives (R8). Call it right after Open, before any Add.
func (ix *Index) RepairKeys(force bool, fn func(key, path string) (string, bool), logger *log.Logger) (int, error) {
	if err := ix.commitPending(); err != nil {
		return 0, err
	}
	if !force && ix.version >= indexVersion {
		return 0, nil
	}
	// Collect first: the single connection is held by the row cursor for the
	// walk, so rows cannot be mutated until it is closed.
	type change struct {
		id     int64
		newKey string
	}
	var changes []change
	rows, err := ix.db.Query(`SELECT id, key, path FROM docs ORDER BY id`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var (
			id        int64
			key, path string
		)
		if err := rows.Scan(&id, &key, &path); err != nil {
			rows.Close()
			return 0, err
		}
		if nk, ok := fn(key, path); ok && nk != key {
			changes = append(changes, change{id: id, newKey: nk})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	if len(changes) > 0 && logger != nil {
		logger.Printf("migrating index keys (%d rows) (one-time upgrade)", len(changes))
	}

	tx, err := ix.db.Begin()
	if err != nil {
		return 0, err
	}
	for _, c := range changes {
		// If a different row already holds the target key (old-binary
		// excursion), drop it first so the re-keyed row can take the unique key.
		var otherID int64
		switch err := tx.QueryRow(`SELECT id FROM docs WHERE key=? AND id<>?`, c.newKey, c.id).Scan(&otherID); {
		case err == nil:
			if err := deleteByIDTx(tx, otherID); err != nil {
				tx.Rollback()
				return 0, err
			}
		case errors.Is(err, sql.ErrNoRows):
		default:
			tx.Rollback()
			return 0, err
		}
		if _, err := tx.Exec(`UPDATE docs SET key=? WHERE id=?`, c.newKey, c.id); err != nil {
			tx.Rollback()
			return 0, err
		}
	}
	// Stamp the current version whenever the repair runs (a fresh row when the
	// legacy index had none), so the next open needs no force.
	if _, err := tx.Exec(`DELETE FROM meta`); err != nil {
		tx.Rollback()
		return 0, err
	}
	if _, err := tx.Exec(`INSERT INTO meta(version) VALUES(?)`, indexVersion); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	ix.version = indexVersion
	return len(changes), nil
}
