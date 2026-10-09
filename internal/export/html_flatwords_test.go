package export_test

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

func flatDivs(n int, inner string) string {
	return strings.Repeat("<div>", n) + inner + strings.Repeat("</div>", n)
}

func flatTables(n int, inner string) string {
	return strings.Repeat(`<table width="100%" cellpadding="0" cellspacing="0"><tr><td>`, n) + inner +
		strings.Repeat("</td></tr></table>", n)
}

// searchTotal is the number of hits the archive's index gives for q.
func searchTotal(t *testing.T, out, q string) int {
	t.Helper()
	ix, err := index.OpenReadonly(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	_, total, err := ix.Search(index.Query{Text: q})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

// covers: MA-477, R8, R7, R13
// Mail flattened past flattenDepth keeps its word boundaries. Where the
// flattener drops a block-level start or end tag (p, div, table, td, li, ...)
// it writes a newline instead, so the words on either side stay apart on the
// page — as a browser shows them, on separate lines — and `reindex -rebuild`,
// which reads the words back from the page, finds the same words the live
// index found in the mail: every word has the same hits before and after a
// rebuild, and no glued token (kestrelalphakestrelbeta) exists in either.
// Driven by 127 nested tables (whose innermost <p>s were rendered intact
// before the flattener), and 495 and 600 nested divs. The twins: a 10-deep
// mail (never flattened) has the same parity, and a word split only by inline
// tags dropped past the limit (kes<b>trel</b>omega) stays ONE word in both
// indexes — a dropped inline tag writes nothing.
func TestFlattenedBlocksKeepWordBoundaries(t *testing.T) {
	out := t.TempDir()
	msgs := []struct{ subject, html string }{
		{"control shallow", flatDivs(10, "<div>kestreleta</div><div>kestreltheta</div>")},
		{"tables 127", flatTables(127, "<p>kestrelalpha</p><p>kestrelbeta</p>")},
		{"divs 495", flatDivs(495, "<div>kestrelgamma</div><div>kestreldelta</div>")},
		{"divs 600", flatDivs(600, "<div>kestrelepsilon</div><div>kestrelzeta</div>")},
		{"inline 495", flatDivs(495, "<p>kes<b>trel</b>omega and <span>kes</span><i>trel</i>sigma</p>")},
	}
	words := []string{"kestreleta", "kestreltheta", "kestrelalpha", "kestrelbeta", "kestrelgamma",
		"kestreldelta", "kestrelepsilon", "kestrelzeta", "kestrelomega", "kestrelsigma"}
	glued := []string{"kestreletakestreltheta", "kestrelalphakestrelbeta", "kestrelgammakestreldelta",
		"kestrelepsilonkestrelzeta"}

	idx, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	pages := map[string]string{}
	exp := &export.Exporter{
		OutDir: out, Manifest: manifest, Mode: export.Full, Log: log.New(io.Discard, "", 0),
		OnExported: func(store string, fp []string, m *model.Message, rel, key string) {
			pages[m.Subject] = rel
			if err := idx.Add(store, fp, m, rel, key); err != nil {
				t.Fatalf("index add: %v", err)
			}
		},
	}
	for i, mm := range msgs {
		m := &model.Message{Subject: mm.subject, SenderEmail: "glue@example.com",
			InternetMessageID: "<glue-" + mm.subject + "@x>",
			Received:          time.Date(2025, 4, 2, 9, 30+i, 0, 0, time.UTC),
			HTMLBody:          "<html><body>" + mm.html + "</body></html>"}
		if _, err := exp.Export("Store", []string{"Inbox"}, m); err != nil {
			t.Fatalf("export %s: %v", mm.subject, err)
		}
	}
	if exp.Stats.HTMLFallback != 0 {
		t.Fatalf("every message must render as HTML (flattened), got %d fallbacks: %+v", exp.Stats.HTMLFallback, exp.Issues)
	}
	if err := idx.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(); err != nil {
		t.Fatal(err)
	}

	// On the page itself: block-separated words are apart, the inline-split
	// word is whole.
	for _, subj := range []string{"tables 127", "divs 495", "divs 600"} {
		b, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(assure.Reached(t, pages[subj], subj+" page"))))
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range glued {
			if strings.Contains(string(b), g) {
				t.Errorf("%s: the page runs block-separated words together (%q)", subj, g)
			}
		}
	}

	live := map[string]int{}
	for _, w := range append(append([]string{}, words...), glued...) {
		live[w] = searchTotal(t, out, w)
	}
	for _, w := range words {
		if live[w] != 1 {
			t.Fatalf("live-index sanity: %q = %d, want 1 (%v)", w, live[w], live)
		}
	}

	for _, s := range []string{"search.db", "search.db-wal", "search.db-shm"} {
		if err := os.Remove(filepath.Join(out, s)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	rep, err := app.Rebuild(out, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if rep.Rebuilt != len(msgs) {
		t.Fatalf("rebuild report = %+v, want Rebuilt=%d", rep, len(msgs))
	}
	for _, w := range append(append([]string{}, words...), glued...) {
		if got := searchTotal(t, out, w); got != live[w] {
			t.Errorf("post-rebuild %q = %d, want %d (the same hits as the live index)", w, got, live[w])
		}
	}
	for _, g := range glued {
		if live[g] != 0 {
			t.Errorf("live index: glued token %q = %d, want 0", g, live[g])
		}
	}

	// HTML's other block-level containers keep their words apart on the page
	// too, as a browser shows them. Page only: the index does not split dt
	// and dd at any depth (its word-boundary set lacks them), so there is no
	// parity to hold here.
	rr, err := export.RenderWith(&model.Message{Subject: "dl",
		HTMLBody: flatDivs(495, "<dl><dt>kestrelmu</dt><dd>kestrelnu</dd></dl>")}, export.RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	if page := string(rr.HTML); rr.BodyFallback != "" || strings.Contains(page, "kestrelmukestrelnu") ||
		!strings.Contains(page, "kestrelmu") || !strings.Contains(page, "kestrelnu") {
		t.Errorf("a dropped <dt>/<dd> must leave its words apart on the rendered page (fallback %q)", rr.BodyFallback)
	}
}
