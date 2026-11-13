package index

import (
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// Gmail folder membership in the search index (docs/design-gmail-dedup.md §8.2 and its folds,
// slice B3). One Gmail email is one docs row, in its home folder (docs.folder); every OTHER
// folder it is in is one doc_folders row. A folder filter, the folder facets and a folder's
// list match either, so one email is one hit and is found under every folder it is in.
//
// The table is made with its first row, in the same transaction, and that transaction stamps
// the index version 4 (§7.4 fold a, MA-1243): an index nobody switched on never holds the table
// and stays version 3, which v1.6.1 and 1.7.x open.

// membersTable is the table's name.
const membersTable = "doc_folders"

// membersSchema makes the table and its two indexes: by folder (a folder's list, its facet) and
// by key (a row's delete, a result's folders).
const membersSchema = `
CREATE TABLE IF NOT EXISTS doc_folders(
  key    TEXT NOT NULL,
  store  TEXT NOT NULL,
  folder TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_doc_folders_folder ON doc_folders(store, folder);
CREATE INDEX IF NOT EXISTS idx_doc_folders_key    ON doc_folders(key);`

// SetMembers makes folders the member folders of the message key of store in the index: the
// key's earlier member rows go, and one row per folder (each once, in order) is written, in the
// batch transaction under its own savepoint (as Add's rows are). The first row ever written makes
// the table and stamps the index version 4 in the same transaction. Folders are the record's
// memberships (state.Record.Folders), never its home folder. With no folders and no table it does
// nothing, so an index nobody switched on is never touched.
func (ix *Index) SetMembers(key, store string, folders []string) error {
	folders = uniqueSorted(folders)
	if len(folders) == 0 && !ix.members && !ix.idle() {
		return nil
	}
	if err := ix.ensure(); err != nil {
		return err
	}
	if len(folders) == 0 && !ix.members {
		return nil
	}
	if ix.tx == nil {
		tx, err := ix.db.Begin()
		if err != nil {
			return err
		}
		ix.tx = tx
	}
	if _, err := ix.tx.Exec(`SAVEPOINT set_members`); err != nil {
		return ix.abandonBatch(err)
	}
	undo := func(cause error) error {
		if _, err := ix.tx.Exec(`ROLLBACK TO set_members`); err != nil {
			return ix.abandonBatch(cause)
		}
		if _, err := ix.tx.Exec(`RELEASE set_members`); err != nil {
			return ix.abandonBatch(cause)
		}
		ix.resyncMembersTx()
		return cause
	}
	if !ix.members {
		if _, err := ix.tx.Exec(membersSchema); err != nil {
			return undo(fmt.Errorf("index members table: %w", err))
		}
		ix.members = true
	}
	if _, err := ix.tx.Exec(`DELETE FROM doc_folders WHERE key=?`, key); err != nil {
		return undo(fmt.Errorf("index members delete: %w", err))
	}
	for _, f := range folders {
		if _, err := ix.tx.Exec(`INSERT INTO doc_folders(key,store,folder) VALUES(?,?,?)`, key, store, f); err != nil {
			return undo(fmt.Errorf("index members insert: %w", err))
		}
	}
	stamped := false
	if len(folders) > 0 && ix.version >= indexVersion && ix.version < indexReadMax {
		if _, err := ix.tx.Exec(`UPDATE meta SET version=?`, indexReadMax); err != nil {
			return undo(fmt.Errorf("index version: %w", err))
		}
		stamped = true
	}
	if _, err := ix.tx.Exec(`RELEASE set_members`); err != nil {
		return ix.abandonBatch(err)
	}
	if stamped {
		ix.version = indexReadMax
	}
	return nil
}

// uniqueSorted is fs with empty and repeated names dropped, sorted: the order a reader lists
// members in after the home folder (folder-tree order, §8.3 fold).
func uniqueSorted(fs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range fs {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// resyncMembers re-reads, after a batch was lost, whether the table exists and the stored
// version: a table made, or a version stamped, inside a lost transaction went with it.
func (ix *Index) resyncMembers() {
	if ix.db == nil {
		return
	}
	ix.members = tableExists(ix.db, membersTable)
	var v sql.NullInt64
	if err := ix.db.QueryRow(`SELECT version FROM meta LIMIT 1`).Scan(&v); err == nil && v.Valid {
		ix.version = int(v.Int64)
	}
}

// seeMembers notices a doc_folders table another connection made since this one opened (a
// reader kept open while a backup writes the first member row). Called with no batch open.
func (ix *Index) seeMembers() {
	if !ix.members && ix.tx == nil && ix.db != nil {
		ix.members = tableExists(ix.db, membersTable)
	}
}

// resyncMembersTx is resyncMembers inside the open batch, after a savepoint was rolled back.
func (ix *Index) resyncMembersTx() {
	var n int
	if err := ix.tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, membersTable).Scan(&n); err == nil {
		ix.members = n > 0
	}
}

// rekeyMembersTx moves the member rows of key old to key nk inside tx (Rekey, RepairKeys).
func rekeyMembersTx(tx *sql.Tx, members bool, old, nk string) error {
	if !members || old == nk {
		return nil
	}
	_, err := tx.Exec(`UPDATE doc_folders SET key=? WHERE key=?`, nk, old)
	return err
}

// Members is the member folders the index holds for key, sorted; none without the table.
func (ix *Index) Members(key string) ([]string, error) {
	if err := ix.ensure(); err != nil {
		return nil, err
	}
	if err := ix.commitPending(); err != nil {
		return nil, err
	}
	ix.seeMembers()
	if !ix.members {
		return nil, nil
	}
	rows, err := ix.db.Query(`SELECT folder FROM doc_folders WHERE key=? ORDER BY folder`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// MemberRow is one email listed in a folder that is not its home: the folder (Store, Folder)
// and the email's own row, whose Path lives in its home folder.
type MemberRow struct {
	Store  string
	Folder string
	PageRow
}

// EachMember streams every member row to fn, ordered by store and folder, and within a folder
// by path descending (newest first, as EachDoc). Nothing without the table.
func (ix *Index) EachMember(fn func(MemberRow) error) error {
	if err := ix.ensure(); err != nil {
		return err
	}
	if err := ix.commitPending(); err != nil {
		return err
	}
	ix.seeMembers()
	if !ix.members {
		return nil
	}
	rows, err := ix.db.Query(`SELECT f.store, f.folder, d.path, d.subject, d.sender_name, d.sender_email, d.date, d.has_attach
		FROM doc_folders f JOIN docs d ON d.key = f.key ORDER BY f.store, f.folder, d.path DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			r    MemberRow
			date int64
			ha   int
		)
		if err := rows.Scan(&r.Store, &r.Folder, &r.Path, &r.Subject, &r.SenderName, &r.SenderEmail, &date, &ha); err != nil {
			return err
		}
		if date > 0 {
			r.Date = time.Unix(date, 0).UTC()
		}
		r.HasAttach = ha != 0
		if err := fn(r); err != nil {
			return err
		}
	}
	return rows.Err()
}
