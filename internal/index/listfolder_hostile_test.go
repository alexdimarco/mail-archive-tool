package index

import (
	"database/sql"
	"strings"
	"testing"
	"unicode/utf8"
)

// covers: MA-435, MA-531, R20, R29
// A search.db handed over holds text the list returns whole and dates the list believes (E4-AO6,
// E4-AO4): three rows with 100 MB cells made a 900 MB list and a reply of the same size, and a date
// past year 9999 became a time that cannot be written into JSON, so that the run record could not be
// made and the export was refused with "disk full or access denied". The list cuts every text cell
// at a bound (at a character, never in the middle of one), reads a key or path that is not a
// manifest key's size as one that matches nothing, and reads a date outside years 1 to 9999 as
// no date. A row inside the bounds is returned as it is (the positive twin).
func TestListFolderBoundsTheTextAndTheDateOfAHostileRow(t *testing.T) {
	path := listFixture(t, 0)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("S", 3<<20)
	wide := strings.Repeat("é", 2<<20) // 4 MB of two-byte characters
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE docs SET subject = ?, sender_name = ?, sender_email = ?, date = 300000000000 WHERE key = ?`, []any{huge, wide, huge, "s\x00Inbox\x00k1"}},
		{`UPDATE docs SET subject = ?, date = 253402300799 WHERE key = ?`, []any{wide, "s\x00Inbox\x00k2"}},
		{`UPDATE docs SET path = ? WHERE key = ?`, []any{huge, "s\x00Inbox\x00k3"}},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	db.Close()

	h, err := OpenForList(path, ListOptions{Immutable: true})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	rows, total, err := h.ListFolder("s", "Inbox", 0, 100)
	if err != nil || total != 3 || len(rows) != 3 {
		t.Fatalf("the list: %d rows, total %d, %v", len(rows), total, err)
	}
	by := map[string]ListRow{}
	for _, r := range rows {
		by[r.Key] = r
	}
	check := func(what, got string, max int) {
		t.Helper()
		if len(got) > max || !utf8.ValidString(got) {
			t.Errorf("%s: %d bytes (valid UTF-8 %v), want at most %d bytes and whole characters", what, len(got), utf8.ValidString(got), max)
		}
		if len(got) == 0 {
			t.Errorf("%s was dropped: it is cut at the bound, not emptied", what)
		}
	}
	k1, k2 := by["s\x00Inbox\x00k1"], by["s\x00Inbox\x00k2"]
	check("a subject of 3 MB", k1.Subject, MaxListSubjectBytes)
	check("a sender name of 4 MB of two-byte characters", k1.SenderName, MaxListSenderBytes)
	check("a sender address of 3 MB", k1.SenderEmail, MaxListSenderBytes)
	check("a subject of 4 MB of two-byte characters", k2.Subject, MaxListSubjectBytes)
	if !k1.Date.IsZero() {
		t.Errorf("a date in year 11476 is read as %v, want no date", k1.Date)
	}
	if k2.Date.IsZero() || k2.Date.Year() != 9999 {
		t.Errorf("the last second of year 9999 is read as %v: a date inside the years the run record can hold is kept", k2.Date)
	}
	if got := by["s\x00Inbox\x00k3"]; len(got.Path) > MaxListKeyBytes {
		t.Errorf("a path of 3 MB: %d bytes returned, want at most %d", len(got.Path), MaxListKeyBytes)
	}
	var each int
	if err := h.EachInFolder("s", "Inbox", func(r ListRow) error { each += len(r.Subject) + len(r.SenderName) + len(r.SenderEmail); return nil }); err != nil {
		t.Fatal(err)
	}
	if each > 4*(MaxListSubjectBytes+2*MaxListSenderBytes) {
		t.Errorf("EachInFolder returned %d bytes of text for three rows: the same bounds apply to the preview's read", each)
	}

	// the positive twin: a row inside every bound comes back as written
	ok, _, err := h.ListFolder("t", "Inbox", 0, 100)
	if err != nil || len(ok) != 1 || ok[0].Subject != "other store" || ok[0].Date.Year() != 2025 {
		t.Errorf("a healthy row: %+v %v", ok, err)
	}
}
