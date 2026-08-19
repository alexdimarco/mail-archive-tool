package index

import (
	"fmt"
	"strings"
	"time"
)

// Query describes a search request.
type Query struct {
	Text      string    // free-text (full-text match); empty means "browse"
	Folder    string    // exact folder or its subtree
	Sender    string    // substring match on sender name/email
	After     time.Time // include items on/after (zero = no bound)
	Before    time.Time // include items strictly before (zero = no bound)
	HasAttach bool      // only messages with attachments
	Sort      string    // "relevance" (default when Text set) or "date"
	Limit     int
	Offset    int
}

// Result is one search hit.
type Result struct {
	Subject     string    `json:"subject"`
	SenderName  string    `json:"senderName"`
	SenderEmail string    `json:"senderEmail"`
	Folder      string    `json:"folder"`
	Date        time.Time `json:"date"`
	Path        string    `json:"path"`
	HasAttach   bool      `json:"hasAttach"`
	Snippet     string    `json:"snippet"` // may contain <mark> highlights
}

// FolderCount is a folder facet with its message count.
type FolderCount struct {
	Folder string `json:"folder"`
	Count  int    `json:"count"`
}

// Search runs the query and returns the page of results plus the total match
// count (ignoring limit/offset).
func (ix *Index) Search(q Query) ([]Result, int, error) {
	if err := ix.commitPending(); err != nil {
		return nil, 0, err
	}
	if q.Limit <= 0 {
		q.Limit = 50
	}

	filters, args := buildFilters(q)
	match := ftsMatch(q.Text)

	var (
		fromWhere string
		selectCol string
		countExpr string
		baseArgs  []any
	)
	if match != "" {
		selectCol = `d.subject, d.sender_name, d.sender_email, d.folder, d.date, d.path, d.has_attach,
			snippet(docs_fts, ` + fmt.Sprint(bodyColumn) + `, '<mark>', '</mark>', '… ', 12)`
		fromWhere = `FROM docs_fts JOIN docs d ON d.id = docs_fts.rowid WHERE docs_fts MATCH ?`
		countExpr = `SELECT count(*) FROM docs_fts JOIN docs d ON d.id = docs_fts.rowid WHERE docs_fts MATCH ?`
		baseArgs = append(baseArgs, match)
	} else {
		selectCol = `d.subject, d.sender_name, d.sender_email, d.folder, d.date, d.path, d.has_attach, d.snippet`
		fromWhere = `FROM docs d WHERE 1=1`
		countExpr = `SELECT count(*) FROM docs d WHERE 1=1`
	}

	order := "d.date DESC"
	if match != "" && q.Sort != "date" {
		order = "bm25(docs_fts)"
	}

	whereArgs := append(append([]any{}, baseArgs...), args...)

	// Total count.
	var total int
	if err := ix.db.QueryRow(countExpr+filters, whereArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count: %w", err)
	}

	// Page of results.
	sqlStr := "SELECT " + selectCol + " " + fromWhere + filters + " ORDER BY " + order + " LIMIT ? OFFSET ?"
	pageArgs := append(append([]any{}, whereArgs...), q.Limit, q.Offset)

	rows, err := ix.db.Query(sqlStr, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var (
			r         Result
			date      int64
			hasAttach int
		)
		if err := rows.Scan(&r.Subject, &r.SenderName, &r.SenderEmail, &r.Folder, &date, &r.Path, &hasAttach, &r.Snippet); err != nil {
			return nil, 0, err
		}
		if date > 0 {
			r.Date = time.Unix(date, 0).UTC()
		}
		r.HasAttach = hasAttach != 0
		results = append(results, r)
	}
	return results, total, rows.Err()
}

// buildFilters returns the SQL fragment and args for the structured filters,
// referencing docs columns via the alias "d".
func buildFilters(q Query) (string, []any) {
	var clauses []string
	var args []any

	if f := strings.TrimSpace(q.Folder); f != "" {
		clauses = append(clauses, "(d.folder = ? OR d.folder LIKE ?)")
		args = append(args, f, f+"/%")
	}
	if s := strings.TrimSpace(q.Sender); s != "" {
		clauses = append(clauses, "(d.sender_name LIKE ? OR d.sender_email LIKE ?)")
		like := "%" + s + "%"
		args = append(args, like, like)
	}
	if !q.After.IsZero() {
		clauses = append(clauses, "d.date >= ?")
		args = append(args, q.After.Unix())
	}
	if !q.Before.IsZero() {
		clauses = append(clauses, "d.date < ?")
		args = append(args, q.Before.Unix())
	}
	if q.HasAttach {
		clauses = append(clauses, "d.has_attach = 1")
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(clauses, " AND "), args
}

// ftsMatch turns free-text into a safe FTS5 MATCH expression by quoting each
// term (implicit AND). Returns "" when there are no usable terms.
func ftsMatch(text string) string {
	fields := strings.Fields(text)
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.ReplaceAll(f, `"`, `""`)
		if f != "" {
			terms = append(terms, `"`+f+`"`)
		}
	}
	return strings.Join(terms, " ")
}

// Count returns the number of indexed messages.
func (ix *Index) Count() (int, error) {
	if err := ix.commitPending(); err != nil {
		return 0, err
	}
	var n int
	err := ix.db.QueryRow(`SELECT count(*) FROM docs`).Scan(&n)
	return n, err
}

// Folders returns the distinct folders with message counts, ordered by folder.
func (ix *Index) Folders() ([]FolderCount, error) {
	if err := ix.commitPending(); err != nil {
		return nil, err
	}
	rows, err := ix.db.Query(`SELECT folder, count(*) FROM docs GROUP BY folder ORDER BY folder`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FolderCount
	for rows.Next() {
		var fc FolderCount
		if err := rows.Scan(&fc.Folder, &fc.Count); err != nil {
			return nil, err
		}
		out = append(out, fc)
	}
	return out, rows.Err()
}

// Years returns the distinct years present (descending), skipping unknown dates.
func (ix *Index) Years() ([]int, error) {
	if err := ix.commitPending(); err != nil {
		return nil, err
	}
	rows, err := ix.db.Query(`SELECT DISTINCT CAST(strftime('%Y', date, 'unixepoch') AS INTEGER) AS y
		FROM docs WHERE date > 0 ORDER BY y DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var y int
		if err := rows.Scan(&y); err != nil {
			return nil, err
		}
		out = append(out, y)
	}
	return out, rows.Err()
}
