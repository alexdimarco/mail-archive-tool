package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// tmpDir is a temp directory with best-effort cleanup: modernc SQLite can hold
// the search.db file briefly past Close on Windows, so a cleanup failure there
// must not fail an otherwise-passing test.
func tmpDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "idxtest")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

// covers: MA-32, R8
// FTS operator/quote input must be treated as literal terms — never an FTS
// syntax error and never an injection.
func TestSearchTreatsOperatorsLiterally(t *testing.T) {
	ix, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if err := ix.Add("s", []string{"Inbox"},
		mkMsg("Quarterly report", "bob", "me", "the quarterly report is attached", time.Unix(1_700_000_000, 0).UTC()),
		"Inbox/x.html", "k1"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{`report OR 1`, `"`, `NEAR(a b)`, `report AND (`, `*`, `report"`, `foo: bar`, `a"b"c`} {
		if _, _, err := ix.Search(Query{Text: q}); err != nil {
			t.Errorf("query %q errored — operators must be matched literally: %v", q, err)
		}
	}

	_, total, err := ix.Search(Query{Text: "quarterly report"})
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, total, "matches for a real multi-term query")
}

func mkMsg(subject, sender, to, body string, date time.Time, attach ...string) *model.Message {
	m := &model.Message{
		Subject:     subject,
		SenderName:  sender,
		SenderEmail: sender + "@example.com",
		To:          to,
		Received:    date,
		HTMLBody:    body,
	}
	for _, a := range attach {
		m.Attachments = append(m.Attachments, model.Attachment{Filename: a})
	}
	return m
}

// covers: MA-23
func TestIndexAddSearch(t *testing.T) {
	path := filepath.Join(tmpDir(t), "search.db")
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	jul := time.Date(2025, 7, 3, 9, 0, 0, 0, time.UTC)
	jun := time.Date(2025, 6, 9, 9, 0, 0, 0, time.UTC)
	old := time.Date(2019, 2, 1, 9, 0, 0, 0, time.UTC)

	msgs := []struct {
		folder string
		m      *model.Message
	}{
		{"Inbox/Clients", mkMsg("Re: Acme invoice #4471", "bob", "me", "<p>Please find the revised <b>invoice</b> for Acme attached.</p>", jul, "invoice.pdf")},
		{"Inbox", mkMsg("Lunch?", "carol", "me", "Are you free for lunch tomorrow?", jun)},
		{"Archive", mkMsg("Acme contract", "bob", "me", "The Acme contract is ready.", old)},
	}
	for i, mm := range msgs {
		key := mm.folder + "\x00id" + string(rune('a'+i))
		if err := ix.Add(mm.m.SenderName+"-store", split(mm.folder), mm.m, mm.folder+"/x.html", key); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}

	// Full-text, ranked.
	res, total, err := ix.Search(Query{Text: "acme invoice"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(res) != 1 || res[0].Subject != "Re: Acme invoice #4471" {
		t.Fatalf("acme invoice search: total=%d res=%+v", total, res)
	}
	if res[0].Snippet == "" {
		t.Error("expected a highlighted snippet")
	}
	if !res[0].HasAttach {
		t.Error("expected hasAttach true")
	}

	// Term appearing in two docs.
	_, total, err = ix.Search(Query{Text: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("acme count = %d, want 2", total)
	}

	// Filter: sender + date window.
	res, total, err = ix.Search(Query{Sender: "bob", After: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || res[0].Subject != "Re: Acme invoice #4471" {
		t.Errorf("sender+date filter: total=%d res=%+v", total, res)
	}

	// Browse (no text) newest-first.
	res, total, err = ix.Search(Query{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || res[0].Subject != "Re: Acme invoice #4471" {
		t.Errorf("browse: total=%d first=%q", total, res[0].Subject)
	}

	// Facets.
	folders, err := ix.Folders()
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 3 {
		t.Errorf("expected 3 folders, got %d", len(folders))
	}
	years, err := ix.Years()
	if err != nil {
		t.Fatal(err)
	}
	if len(years) != 2 || years[0] != 2025 {
		t.Errorf("years = %v, want [2025 2019]", years)
	}

	// Re-adding the same key replaces (no duplicate).
	key := "Inbox\x00idb"
	if err := ix.Add("carol-store", []string{"Inbox"}, mkMsg("Lunch? (updated)", "carol", "me", "moved to Friday", jun), "Inbox/x.html", key); err != nil {
		t.Fatal(err)
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}
	if n, _ := ix.Count(); n != 3 {
		t.Errorf("after replace, count = %d, want 3", n)
	}

	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen read-only and confirm persistence.
	ro, err := OpenReadonly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if n, _ := ro.Count(); n != 3 {
		t.Errorf("readonly count = %d, want 3", n)
	}
}

// covers: MA-43, R13
// EachRow enumerates every stored (id,key,path); DeleteByID removes a row from
// both the metadata table and the aligned FTS index (so it stops matching), and
// DeleteByKey targets a row by its dedup key. This is the plumbing reindex uses
// to prune dangling entries.
func TestEachRowAndDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "search.db")
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()

	now := time.Unix(1_700_000_000, 0).UTC()
	adds := []struct{ folder, body, rel, key string }{
		{"Inbox", "alpha unique-alpha", "Inbox/a.html", "kA"},
		{"Inbox", "beta unique-beta", "Inbox/b.html", "kB"},
		{"Archive", "gamma unique-gamma", "Archive/c.html", "kC"},
	}
	for _, a := range adds {
		if err := ix.Add("store", split(a.folder), mkMsg("s", "x", "me", a.body, now), a.rel, a.key); err != nil {
			t.Fatalf("add %s: %v", a.key, err)
		}
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}

	// EachRow sees every row, with the path and key we stored.
	byKey := map[string]struct {
		id   int64
		path string
	}{}
	if err := ix.EachRow(func(id int64, key, path string) error {
		byKey[key] = struct {
			id   int64
			path string
		}{id, path}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, byKey, "rows enumerated by EachRow")
	if len(byKey) != 3 {
		t.Fatalf("EachRow saw %d rows, want 3", len(byKey))
	}
	if got := byKey["kB"].path; got != "Inbox/b.html" {
		t.Errorf("kB path = %q, want Inbox/b.html", got)
	}

	// DeleteByID removes kB from both docs and docs_fts.
	if err := ix.DeleteByID(byKey["kB"].id); err != nil {
		t.Fatal(err)
	}
	if n, _ := ix.Count(); n != 2 {
		t.Errorf("count after DeleteByID = %d, want 2", n)
	}
	if _, total, err := ix.Search(Query{Text: "unique-beta"}); err != nil || total != 0 {
		t.Errorf("deleted body still matches FTS: total=%d err=%v", total, err)
	}
	if _, total, _ := ix.Search(Query{Text: "unique-alpha"}); total != 1 {
		t.Errorf("surviving body no longer matches: total=%d, want 1", total)
	}

	// DeleteByKey removes kC; deleting an absent key is a no-op.
	if err := ix.DeleteByKey("kC"); err != nil {
		t.Fatal(err)
	}
	if err := ix.DeleteByKey("does-not-exist"); err != nil {
		t.Errorf("DeleteByKey on absent key errored: %v", err)
	}
	if n, _ := ix.Count(); n != 1 {
		t.Errorf("count after DeleteByKey = %d, want 1", n)
	}
}

func split(folder string) []string {
	if folder == "" {
		return nil
	}
	var out []string
	for _, p := range filepathSplit(folder) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func filepathSplit(s string) []string {
	var parts []string
	cur := ""
	for _, r := range s {
		if r == '/' {
			parts = append(parts, cur)
			cur = ""
		} else {
			cur += string(r)
		}
	}
	return append(parts, cur)
}
