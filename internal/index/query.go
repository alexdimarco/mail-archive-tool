package index

import (
	"fmt"
	"html"
	"strings"
	"time"
	"unicode"
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
	Snippet     string    `json:"snippet"` // HTML-escaped text; only the index's own <mark> tags are live
}

// Snippet highlight sentinels: FTS5 wraps matches in these control characters
// (never present in mail text), the text is HTML-escaped, and only then are
// they turned into <mark> tags — so a body containing markup can never reach
// the UI live (R19/R8).
const snipOpen, snipClose = "\x02", "\x03"

// safeSnippet escapes a raw snippet and turns the highlight sentinels into
// <mark> tags.
func safeSnippet(raw string) string {
	s := html.EscapeString(raw)
	s = strings.ReplaceAll(s, snipOpen, "<mark>")
	return strings.ReplaceAll(s, snipClose, "</mark>")
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
		fromWhere  string
		selectCol  string
		countExpr  string
		baseArgs   []any
		selectArgs []any // bound parameters used by the SELECT list (before the WHERE args)
	)
	if match != "" {
		selectCol = `d.subject, d.sender_name, d.sender_email, d.folder, d.date, d.path, d.has_attach,
			snippet(docs_fts, ` + fmt.Sprint(bodyColumn) + `, ?, ?, '… ', 12)`
		selectArgs = []any{snipOpen, snipClose}
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
	pageArgs := append(append(append([]any{}, selectArgs...), whereArgs...), q.Limit, q.Offset)

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
		r.Snippet = safeSnippet(r.Snippet)
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

// ParseQuery folds the inline search tokens in text into base and returns the
// resulting Query, with the leftover words as its free-text Text. This is the
// one grammar shared by the terminal `search` verb and the serve web box, so a
// query typed in either place means the same thing. Recognised tokens:
//
//	from:NAME        substring match on sender name/email (→ Sender)
//	folder:PATH      a folder and its subtree             (→ Folder)
//	after:DATE       on/after DATE                        (→ After)
//	before:DATE      strictly before DATE                 (→ Before)
//	has:attach       only messages with attachments       (→ HasAttach)
//	has:attachment   alias of has:attach
//
// A token overrides the same field already set in base — the token wins over an
// explicit flag/param naming a different value. Dates accept the partial forms
// ParseDate accepts (a bare year or year-month), so `after:2025-01` is a real
// month bound rather than being silently dropped. A token whose date does not
// parse is ignored (the rest of the query still runs).
//
// A token value may be quoted so it can carry spaces: folder:"Sent Messages" and
// from:'a b' are one token each, not a token plus a dropped free term. The quote
// characters are grouping delimiters (stripped from the value); an unterminated
// quote runs to the end of the input.
func ParseQuery(text string, base Query) Query {
	var terms []string
	for _, f := range tokenizeQuery(text) {
		low := strings.ToLower(f)
		switch {
		case strings.HasPrefix(low, "from:"):
			base.Sender = f[len("from:"):]
		case strings.HasPrefix(low, "folder:"):
			base.Folder = f[len("folder:"):]
		case strings.HasPrefix(low, "after:"):
			if t, ok := ParseDate(f[len("after:"):]); ok {
				base.After = t
			}
		case strings.HasPrefix(low, "before:"):
			if t, ok := ParseDate(f[len("before:"):]); ok {
				base.Before = t
			}
		case low == "has:attach" || low == "has:attachment":
			base.HasAttach = true
		default:
			terms = append(terms, f)
		}
	}
	base.Text = strings.Join(terms, " ")
	return base
}

// tokenizeQuery splits a query into whitespace-separated tokens, but a single-
// or double-quoted span keeps its spaces inside one token so a value like
// folder:"Sent Messages" survives intact (friction #6). The quote characters
// themselves are delimiters and are not part of the token; an unterminated quote
// extends to the end of the input. With no quotes this is exactly strings.Fields.
func tokenizeQuery(text string) []string {
	var tokens []string
	var cur strings.Builder
	inTok := false
	var quote rune // the open quote rune, or 0 when not inside a quote
	flush := func() {
		if inTok {
			tokens = append(tokens, cur.String())
			cur.Reset()
			inTok = false
		}
	}
	for _, r := range text {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0 // closing quote: drop it, stay in the same token
			} else {
				cur.WriteRune(r)
			}
			inTok = true
		case r == '"' || r == '\'':
			quote = r
			inTok = true // an empty quoted value is still a token
		case unicode.IsSpace(r):
			flush()
		default:
			cur.WriteRune(r)
			inTok = true
		}
	}
	flush()
	return tokens
}

// ParseDate parses a search date bound. It accepts a full RFC3339 timestamp, a
// calendar day (2006-01-02), a month (2006-01 → its first day) or a bare year
// (2006 → January 1), each interpreted in UTC to match the index's stored times.
// The second result is false for empty or unrecognised input, so a bad bound is
// dropped rather than crashing the query.
func ParseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
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

// Range returns the oldest and newest indexed message dates (UTC), over docs
// with a known date (date > 0); ok is false when the index holds no dated
// messages. `status` prints it as the archive's coverage (product P6-windows).
func (ix *Index) Range() (oldest, newest time.Time, ok bool) {
	if err := ix.commitPending(); err != nil {
		return time.Time{}, time.Time{}, false
	}
	// MIN/MAX over no rows are NULL; a **int64 receives that as nil.
	var lo, hi *int64
	if err := ix.db.QueryRow(`SELECT MIN(date), MAX(date) FROM docs WHERE date > 0`).Scan(&lo, &hi); err != nil {
		return time.Time{}, time.Time{}, false
	}
	if lo == nil || hi == nil {
		return time.Time{}, time.Time{}, false
	}
	return time.Unix(*lo, 0).UTC(), time.Unix(*hi, 0).UTC(), true
}
