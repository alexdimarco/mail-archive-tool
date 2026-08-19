package index

import "time"

// PageRow is the minimal per-message data used to build static folder pages.
type PageRow struct {
	Path        string
	Subject     string
	SenderName  string
	SenderEmail string
	Date        time.Time
	HasAttach   bool
}

// EachDoc streams every indexed message to fn, ordered by path descending so
// rows within a directory are contiguous and newest-first (filenames begin with
// the date). It streams rather than materializing, so it scales to large
// archives.
func (ix *Index) EachDoc(fn func(PageRow) error) error {
	if err := ix.commitPending(); err != nil {
		return err
	}
	rows, err := ix.db.Query(`SELECT path, subject, sender_name, sender_email, date, has_attach
		FROM docs ORDER BY path DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			r    PageRow
			date int64
			ha   int
		)
		if err := rows.Scan(&r.Path, &r.Subject, &r.SenderName, &r.SenderEmail, &date, &ha); err != nil {
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
