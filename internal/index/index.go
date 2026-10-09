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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

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

// ErrNewerFormat is wrapped by Open's refusal of a search.db written by a
// newer mailarchive (a stored format version above indexVersion). That refusal
// names the file and the upgrade itself, so a caller can tell it from a
// damaged file, whose remedy is the rebuild — which would replace a newer
// index with an older one.
var ErrNewerFormat = errors.New("written by a newer mailarchive")

// Index is an open search index.
type Index struct {
	db      *sql.DB
	tx      *sql.Tx
	pending int
	lost    int // rows whose Add succeeded but whose batch was later lost (see Lost)
	version int // key-format version read at Open (see indexVersion)
}

// Open opens (creating if needed) a writable index at path and begins a batch.
// It opens through the same escaped file: URI as OpenReadonly (fileURI), so a
// '?', '#' or '%' in the archive path is part of the file name — never URI
// parameters that cut the path short — and its connection waits out a lock
// another connection holds for a moment (writerBusyTimeout): a status, search
// or serve read-only open running WAL recovery, or a writer committing. Without
// that wait such a moment failed the open with SQLITE_BUSY(_RECOVERY), and with
// it the whole export run. A damaged file is not busy and still fails at once.
func Open(path string) (*Index, error) {
	dsn, err := fileURI(path, "mode=rwc", writerBusyTimeout)
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	db, err := sql.Open("sqlite", dsn)
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
		return nil, fmt.Errorf("search index %s was %w (format version %d; this build understands %d): upgrade this copy of mailarchive, or point -out at an archive this version wrote", path, ErrNewerFormat, version, indexVersion)
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

// OpenReadonly opens an existing index for querying (serve/search/status). The
// connection is genuinely read-only (SQLite URI mode=ro): it can never write
// search.db, even by checkpointing a crashed writer's WAL. A WAL database needs
// its -shm to be read, so on read-only media (a DVD, a write-protected USB
// stick, a read-only share) where none exists and none can be created, the open
// is retried as immutable=1 — safe there because nothing can be writing the
// file — instead of failing with SQLITE_READONLY_DIRECTORY (F85/N14). On a
// writable archive with no live writer, a read-only WAL reader cannot remove
// the empty -wal/-shm pair it needed; the next writer reclaims them.
func OpenReadonly(path string) (*Index, error) {
	// Check for the file first, so a missing index yields a clean "run an export
	// first" rather than the SQLite driver's misleading low-level open error.
	if _, statErr := os.Stat(path); errors.Is(statErr, fs.ErrNotExist) {
		return nil, fmt.Errorf("no search index at %s (run an export first)", path)
	}
	db, err := openProbed(path, "mode=ro")
	if err != nil && readonlyMediaErr(err) {
		db, err = openProbed(path, "mode=ro&immutable=1")
	}
	if err != nil && Busy(err) {
		// In use, not damaged: the rebuild remedy would be wrong advice.
		return nil, fmt.Errorf("search index %s is busy (%w): another program or run kept it locked for longer than %s — try again in a moment", path, err, readonlyBusyTimeout)
	}
	if err != nil {
		return nil, &UnreadableError{Path: path, Cause: err}
	}
	return &Index{db: db}, nil
}

// UnreadableError is OpenReadonly's refusal of a search.db that is present but
// cannot be read (damaged, not a database, a table missing). Its message names
// the file, SQLite's own cause and the rebuild remedy; Cause alone is that root
// cause, for a caller such as `status` that names the file and the remedy
// itself and must not print either twice.
type UnreadableError struct {
	Path  string // the search.db that could not be read
	Cause error  // SQLite's (or the driver's) own error
}

func (e *UnreadableError) Error() string {
	return fmt.Sprintf("search index %s is present but cannot be read (%v): if it is damaged, rebuild it from the archive with `mailarchive reindex -rebuild -out %q` (on read-only media, copy the archive to a writable location first)", e.Path, e.Cause, filepath.Dir(e.Path))
}

// Unwrap exposes Cause, so errors.As still reaches the *sqlite.Error.
func (e *UnreadableError) Unwrap() error { return e.Cause }

// readonlyBusyTimeout is how long a read-only open waits for a lock another
// connection holds before giving up with SQLITE_BUSY: a writer committing or
// checkpointing, or the WAL recovery the next opener runs after a read-only
// reader left the -wal/-shm pair behind. Without it every such moment failed
// the open at once, and `status` reported a healthy index as unreadable. A
// var so a test can shrink it.
var readonlyBusyTimeout = 5 * time.Second

// writerBusyTimeout is the same wait for the writable connection Open makes
// (an export, a Graph run, reindex): every statement on it waits out a lock
// another connection holds for up to this long before SQLITE_BUSY. A var so a
// test can change it.
var writerBusyTimeout = 5 * time.Second

// Busy reports whether err is SQLite's "in use" answer — SQLITE_BUSY (with its
// BUSY_RECOVERY/BUSY_SNAPSHOT variants) or SQLITE_LOCKED, by primary result
// code — which says another connection held the database past the busy
// timeout, and nothing about whether the file is damaged.
func Busy(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	switch se.Code() & 0xff {
	case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
		return true
	}
	return false
}

// fileURI is the SQLite file: URI for the database at path, carrying the given
// query (e.g. "mode=ro") and a busy timeout of busy. It is built from the
// absolute path with every special character percent-escaped, so a '?', '#'
// or '%' in an archive path is never parsed as a URI parameter. The busy
// timeout rides on the URI as the driver's _pragma parameter (SQLite ignores
// it), so it is set on every connection the pool opens.
func fileURI(path, query string, busy time.Duration) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // a Windows drive path: file:///C:/...
	}
	query += fmt.Sprintf("&_pragma=busy_timeout(%d)", busy.Milliseconds())
	u := url.URL{Scheme: "file", Path: p, RawQuery: query}
	return u.String(), nil
}

// openProbed opens the database at path through its file: URI (fileURI)
// carrying the given query and the read-only busy timeout, and probes that
// docs exists and is readable.
func openProbed(path, query string) (*sql.DB, error) {
	dsn, err := fileURI(path, query, readonlyBusyTimeout)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Probe, fully consuming the one-row result so the connection (and its OS
	// file handle) is returned to the pool. The old db.Query discarded an
	// unclosed *sql.Rows that pinned a connection open past ix.Close():
	// harmless on POSIX, but on Windows it blocks a later os.Remove/os.Rename
	// of search.db (a Rebuild then fails "Access is denied"). An empty index is
	// still usable, so sql.ErrNoRows counts as success.
	var probe int
	switch err := db.QueryRow(`SELECT 1 FROM docs LIMIT 1`).Scan(&probe); {
	case err == nil, errors.Is(err, sql.ErrNoRows):
		return db, nil
	default:
		db.Close()
		return nil, err
	}
}

// readonlyMediaErr reports whether a read-only open failed because the media
// cannot host the WAL reader's -shm (SQLITE_READONLY_* or SQLITE_CANTOPEN, by
// primary result code), the one case the immutable retry is for. A corrupt
// file (SQLITE_NOTADB) or a missing table is not retried.
func readonlyMediaErr(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	switch se.Code() & 0xff {
	case sqlite3.SQLITE_READONLY, sqlite3.SQLITE_CANTOPEN:
		return true
	}
	return false
}

// Add records (or replaces, by key) one message in the index. Each row is
// written under its own SAVEPOINT inside the batch transaction, so a failure is
// isolated to that row (F49): it is rolled back to the savepoint — leaving no
// docs row without its docs_fts twin (INT-2) — while the earlier rows of the
// uncommitted batch stay and commit with it. The returned error reports that
// one row. If SQLite itself abandoned the whole transaction (a RAISE(ROLLBACK),
// or a disk-full/I-O error it may answer that way), the earlier rows went with
// it: the error says so and they are counted in Lost.
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

	if _, err := ix.tx.Exec(`SAVEPOINT add_row`); err != nil {
		return ix.abandonBatch(err)
	}
	// Replace any existing row with the same key (full-mode re-export).
	if err := deleteByKeyTx(ix.tx, key); err != nil {
		return ix.rollbackRow(err)
	}

	res, err := ix.tx.Exec(`INSERT INTO docs(key,store,folder,sender_name,sender_email,recipients,subject,date,path,has_attach,attach_names,snippet)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		key, store, folder, m.SenderName, m.SenderEmail, recipients, m.Subject, date, relPath, boolToInt(len(m.Attachments) > 0), attach, snippet)
	if err != nil {
		return ix.rollbackRow(fmt.Errorf("index insert: %w", err))
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ix.rollbackRow(err)
	}
	if _, err := ix.tx.Exec(`INSERT INTO docs_fts(rowid,subject,sender,recipients,folder,attachments,body)
		VALUES(?,?,?,?,?,?,?)`, id, m.Subject, sender, recipients, folder, attach, body); err != nil {
		// A docs row inserted without its docs_fts twin would leave the two
		// tables misaligned; rolling back to the savepoint removes it, so
		// neither a later Flush nor Close can commit the orphan (INT-2).
		return ix.rollbackRow(fmt.Errorf("index fts insert: %w", err))
	}
	if _, err := ix.tx.Exec(`RELEASE add_row`); err != nil {
		return ix.abandonBatch(err)
	}

	ix.pending++
	if ix.pending >= batchSize {
		return ix.commitBatch(1) // this row's loss, if any, is reported by the error
	}
	return nil
}

// rollbackRow undoes the failed row back to its savepoint, keeping the rest of
// the batch, and returns cause. When the savepoint is gone because SQLite has
// already abandoned the whole transaction, the batch is abandoned and counted
// as lost (abandonBatch).
func (ix *Index) rollbackRow(cause error) error {
	if _, err := ix.tx.Exec(`ROLLBACK TO add_row`); err != nil {
		return ix.abandonBatch(cause)
	}
	if _, err := ix.tx.Exec(`RELEASE add_row`); err != nil {
		return ix.abandonBatch(cause)
	}
	return cause
}

// abandonBatch rolls back the current uncommitted batch and clears it, so a row
// that failed mid-insert can never be committed by a later Flush/Close and the
// next Add begins a fresh transaction. It is reached only when the batch
// transaction itself is lost, so the earlier rows of the batch (whose Add had
// succeeded) are counted in Lost and named in the returned error, which wraps
// cause.
func (ix *Index) abandonBatch(cause error) error {
	if ix.tx == nil {
		return cause
	}
	_ = ix.tx.Rollback()
	ix.tx = nil
	lost := ix.pending
	ix.pending = 0
	if lost == 0 {
		return cause
	}
	ix.lost += lost
	return fmt.Errorf("%w (the batch transaction was lost with it: %d earlier message(s) were not indexed)", cause, lost)
}

// commitPending commits the current batch (if any) and releases the connection.
// A new transaction is begun lazily on the next Add. If the commit fails, the
// whole batch is lost and its rows are counted in Lost.
func (ix *Index) commitPending() error {
	return ix.commitBatch(0)
}

// commitBatch is commitPending for a caller whose own error already reports
// `reported` of the pending rows (Add committing at the batch size reports its
// own row); only the rest are added to Lost on a failed commit.
func (ix *Index) commitBatch(reported int) error {
	if ix.tx == nil {
		return nil
	}
	err := ix.tx.Commit()
	if err != nil {
		ix.lost += ix.pending - reported
	}
	ix.tx = nil
	ix.pending = 0
	return err
}

// Lost is how many rows this Index accepted (their Add returned nil) but never
// committed, because their batch transaction was lost afterwards — SQLite
// abandoned it on a later row's failure, or the commit itself failed. A caller
// counting index errors adds it to the failed Adds it saw, so no lost row goes
// uncounted (R8).
func (ix *Index) Lost() int {
	return ix.lost
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

// Rekey applies the v3→v5 identity-collapse to the search index in ONE
// transaction: it renames each row old→new for every entry in remap (the
// survivors) and DELETES every key in drop (the collapsed-away losers), then
// checkpoints the WAL into the main db file before returning — so the index
// migration is durable BEFORE the manifest advances (EC5), and a crash between
// the two leaves the index ahead, which the idempotent collapse heals on the next
// run. Drop-the-occupant collision semantics match RepairKeys: if another row
// already holds a target key, it is removed first. A remap source that no longer
// exists (already re-keyed) is skipped, so Rekey is itself idempotent.
func (ix *Index) Rekey(remap map[string]string, drop []string) error {
	if err := ix.commitPending(); err != nil {
		return err
	}
	if len(remap) == 0 && len(drop) == 0 {
		return nil
	}
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	for _, k := range drop {
		if err := deleteByKeyTx(tx, k); err != nil {
			tx.Rollback()
			return err
		}
	}
	for old, nk := range remap {
		var id int64
		switch err := tx.QueryRow(`SELECT id FROM docs WHERE key=?`, old).Scan(&id); {
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
			continue // already re-keyed (idempotent) or the row was never indexed
		default:
			tx.Rollback()
			return err
		}
		var otherID int64
		switch err := tx.QueryRow(`SELECT id FROM docs WHERE key=? AND id<>?`, nk, id).Scan(&otherID); {
		case err == nil:
			if err := deleteByIDTx(tx, otherID); err != nil {
				tx.Rollback()
				return err
			}
		case errors.Is(err, sql.ErrNoRows):
		default:
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`UPDATE docs SET key=? WHERE id=?`, nk, id); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, _ = ix.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`) // durable before the manifest advances
	return nil
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
