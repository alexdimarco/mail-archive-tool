package index

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync/atomic"
	"time"
)

// The extraction workspace's message list (design §4.3, XC19, XC20, RR2-C11): one folder
// of one store, read from search.db through a connection that can write nothing. The
// index is opened for one request and closed at its end, never held across requests, so
// the workspace never keeps search.db open inside Rebuild's rename window; the open
// handles are counted so a test can see one that is not closed.

// MaxListRows is the most rows one ListFolder page returns, whatever the caller asks.
const MaxListRows = 100

// ListRow is one indexed message of a folder, with the manifest key that names it.
type ListRow struct {
	Key         string
	Path        string
	Subject     string
	SenderName  string
	SenderEmail string
	Date        time.Time // zero when the message has no date
	HasAttach   bool
}

// ListOptions say how a list opens search.db.
type ListOptions struct {
	// Immutable opens the file with immutable=1, so that SQLite takes no lock and creates
	// no -wal, -shm or journal beside it. The extraction workspace sets it for another
	// archive folder (RR2-C11), where nothing may ever be written; a -wal beside the file
	// is then not read (the caller says so). Without it the index opens mode=ro, and
	// reopens immutable only where the media cannot host a read-only reader's -shm.
	Immutable bool
}

// ListUnavailableError is a list's refusal of an index it cannot use: a file that is not a
// database, a `docs` that is not a table, a `docs` without the columns this build defines,
// or one SQLite will not read. Whole-folder selection still works from the manifest.
type ListUnavailableError struct {
	Path   string
	Reason string
	Cause  error
}

func (e *ListUnavailableError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("search index %s can't be used for a message list (%s: %v)", e.Path, e.Reason, e.Cause)
	}
	return fmt.Sprintf("search index %s can't be used for a message list (%s)", e.Path, e.Reason)
}

// Unwrap exposes Cause, so Busy and errors.As still reach SQLite's own error.
func (e *ListUnavailableError) Unwrap() error { return e.Cause }

var openLists atomic.Int64

// OpenListHandles is how many list handles are open now: zero between requests.
func OpenListHandles() int { return int(openLists.Load()) }

// ListHandle is an index open for one list request. The caller closes it at the end of
// the request.
type ListHandle struct {
	db      *sql.DB
	Version int // the index's key-format version (1 when it has no meta table)
	closed  atomic.Bool
}

// listColumns are the docs columns a list reads.
var listColumns = []string{"id", "key", "store", "folder", "sender_name", "sender_email", "subject", "date", "path", "has_attach"}

// listPragmas make the connection one that cannot write, and take SQLite's settings for a
// file that may be hostile: no unsafe functions in a schema, a check of every cell, no
// memory map (RR2-C11).
const listPragmas = "&_pragma=query_only(1)&_pragma=trusted_schema(0)&_pragma=cell_size_check(1)&_pragma=mmap_size(0)"

// OpenForList opens the index at path for a list request: read-only, and, with
// opt.Immutable, without a lock or a sidecar. A stored version above this build's is
// refused with ErrNewerFormat, a missing file with fs.ErrNotExist, and a file it cannot
// use with *ListUnavailableError, each before any query of the folder.
func OpenForList(path string, opt ListOptions) (*ListHandle, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no search index at %s: %w", path, fs.ErrNotExist)
	}
	query := "mode=ro"
	if opt.Immutable {
		query += "&immutable=1"
	}
	h, err := openListQuery(path, query)
	if err != nil && !opt.Immutable && readonlyMediaErr(unwrapCause(err)) {
		h, err = openListQuery(path, "mode=ro&immutable=1")
	}
	if err != nil {
		return nil, err
	}
	openLists.Add(1)
	return h, nil
}

func unwrapCause(err error) error {
	var ue *ListUnavailableError
	if errors.As(err, &ue) && ue.Cause != nil {
		return ue.Cause
	}
	return err
}

func openListQuery(path, query string) (*ListHandle, error) {
	dsn, err := fileURI(path, query+listPragmas, readonlyBusyTimeout)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, &ListUnavailableError{Path: path, Reason: "it could not be opened", Cause: err}
	}
	db.SetMaxOpenConns(1)
	unusable := func(reason string, cause error) (*ListHandle, error) {
		db.Close()
		return nil, &ListUnavailableError{Path: path, Reason: reason, Cause: cause}
	}
	// docs must be a table (never a view, which a hostile file could make run its own
	// query), with the columns this build's index versions define.
	var typ string
	switch err := db.QueryRow(`SELECT type FROM sqlite_master WHERE name = 'docs'`).Scan(&typ); {
	case errors.Is(err, sql.ErrNoRows):
		return unusable("it has no docs table", nil)
	case err != nil:
		return unusable("it can't be read", err)
	}
	if typ != "table" {
		return unusable("its docs is a "+typ+", not a table", nil)
	}
	have := map[string]bool{}
	rows, err := db.Query(`SELECT name FROM pragma_table_info('docs')`)
	if err != nil {
		return unusable("it can't be read", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return unusable("it can't be read", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return unusable("it can't be read", err)
	}
	rows.Close()
	for _, c := range listColumns {
		if !have[c] {
			return unusable("its docs table has no "+c+" column", nil)
		}
	}
	version := 1 // no meta table: a pre-meta index, whose keys the caller maps
	if tableExists(db, "meta") {
		var v sql.NullInt64
		switch err := db.QueryRow(`SELECT version FROM meta LIMIT 1`).Scan(&v); {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return unusable("its version can't be read", err)
		case v.Valid:
			version = int(v.Int64)
		}
	}
	if version > indexVersion {
		db.Close()
		return nil, &NewerIndexError{Path: path, Version: version, Understood: indexVersion}
	}
	return &ListHandle{db: db, Version: version}, nil
}

// NewerIndexError is a list's refusal of a search.db a newer mailarchive wrote. It is
// ErrNewerFormat, with the version, for the caller that words the refusal itself.
type NewerIndexError struct {
	Path       string
	Version    int // the format version the file holds
	Understood int // the version this build understands
}

func (e *NewerIndexError) Error() string {
	return fmt.Sprintf("search index %s was %v (format version %d; this build understands %d): upgrade this copy of mailarchive, or point -out at an archive this version wrote", e.Path, ErrNewerFormat, e.Version, e.Understood)
}

func (e *NewerIndexError) Unwrap() error { return ErrNewerFormat }

// Close releases the index. It is safe to call twice.
func (h *ListHandle) Close() error {
	if !h.closed.CompareAndSwap(false, true) {
		return nil
	}
	openLists.Add(-1)
	return h.db.Close()
}

const listSelect = `SELECT key, path, subject, sender_name, sender_email, date, has_attach FROM docs WHERE store = ? AND folder = ? ORDER BY date DESC, id DESC`

// ListFolder is one page of the messages of folder in store (an exact match: a subfolder's
// messages are not its parent's), newest first, with the folder's total. A page holds at
// most MaxListRows rows however many are asked for.
func (h *ListHandle) ListFolder(store, folder string, offset, limit int) ([]ListRow, int, error) {
	if limit <= 0 || limit > MaxListRows {
		limit = MaxListRows
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := h.db.QueryRow(`SELECT count(*) FROM docs WHERE store = ? AND folder = ?`, store, folder).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := h.db.Query(listSelect+` LIMIT ? OFFSET ?`, store, folder, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []ListRow
	for rows.Next() {
		r, err := scanListRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// EachInFolder streams every message of folder in store, newest first, to fn: the metadata
// a preview of a whole folder needs, without holding a page of rows. A non-nil error from
// fn stops it.
func (h *ListHandle) EachInFolder(store, folder string, fn func(ListRow) error) error {
	rows, err := h.db.Query(listSelect, store, folder)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanListRow(rows)
		if err != nil {
			return err
		}
		if err := fn(r); err != nil {
			return err
		}
	}
	return rows.Err()
}

func scanListRow(rows *sql.Rows) (ListRow, error) {
	var (
		r                              ListRow
		key, path, subj, sname, semail sql.NullString
		date, hasAttach                sql.NullInt64
	)
	if err := rows.Scan(&key, &path, &subj, &sname, &semail, &date, &hasAttach); err != nil {
		return r, err
	}
	r.Key, r.Path, r.Subject, r.SenderName, r.SenderEmail = key.String, path.String, subj.String, sname.String, semail.String
	if date.Valid && date.Int64 > 0 {
		r.Date = time.Unix(date.Int64, 0).UTC()
	}
	r.HasAttach = hasAttach.Valid && hasAttach.Int64 != 0
	return r, nil
}
