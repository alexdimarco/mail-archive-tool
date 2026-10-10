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
// before the flattener), 495 and 600 nested divs, and a 495-deep <dl> whose
// <dt>/<dd> now split words on both indexes (index.blockElements was widened to
// the flattener's block set, so dt/dd/dl/pre/address/... read the same live and
// rebuilt). The twins: a 10-deep mail (never flattened) has the same parity,
// and a word split only by inline tags dropped past the limit
// (kes<b>trel</b>omega) stays ONE word in both indexes — a dropped inline tag
// writes nothing.
func TestFlattenedBlocksKeepWordBoundaries(t *testing.T) {
	out := t.TempDir()
	msgs := []struct{ subject, html string }{
		{"control shallow", flatDivs(10, "<div>kestreleta</div><div>kestreltheta</div>")},
		{"tables 127", flatTables(127, "<p>kestrelalpha</p><p>kestrelbeta</p>")},
		{"divs 495", flatDivs(495, "<div>kestrelgamma</div><div>kestreldelta</div>")},
		{"divs 600", flatDivs(600, "<div>kestrelepsilon</div><div>kestrelzeta</div>")},
		{"inline 495", flatDivs(495, "<p>kes<b>trel</b>omega and <span>kes</span><i>trel</i>sigma</p>")},
		{"dl 495", flatDivs(495, "<dl><dt>kestrelmu</dt><dd>kestrelnu</dd></dl>")},
	}
	words := []string{"kestreleta", "kestreltheta", "kestrelalpha", "kestrelbeta", "kestrelgamma",
		"kestreldelta", "kestrelepsilon", "kestrelzeta", "kestrelomega", "kestrelsigma",
		"kestrelmu", "kestrelnu"}
	glued := []string{"kestreletakestreltheta", "kestrelalphakestrelbeta", "kestrelgammakestreldelta",
		"kestrelepsilonkestrelzeta", "kestrelmukestrelnu"}

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
	for _, subj := range []string{"tables 127", "divs 495", "divs 600", "dl 495"} {
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

}

// covers: MA-477, R8
// The flattener and the search index split words at the same names. Where the
// flattener drops a block-level tag past flattenDepth it writes a newline
// (export flatBlock), and the index splits words at its own set (index
// blockElements); the live index reads the mail, `reindex -rebuild` the
// flattened page, so a name in one set and not in the other splits two words
// on one side and glues them on the other (R8). 0f824b9 made the sets equal,
// but the end-to-end test above pins that only for the names it drives (div,
// p, the table parts, dl/dt/dd): `"pre": false` in blockElements left export,
// index and app green. Each package lists its set (export.FlatBlockNames,
// index.BlockElementNames), and the two lists must be equal, name for name, in
// either direction. Positive twin: both hold names the end-to-end test drives,
// and pre.
func TestFlattenerAndIndexSplitWordsAtTheSameNames(t *testing.T) {
	flat, idx := export.FlatBlockNames(), index.BlockElementNames()
	assure.Reached(t, len(flat), "the flattener's block names")
	assure.Reached(t, len(idx), "the index's block names")
	in := func(names []string) map[string]bool {
		set := map[string]bool{}
		for _, n := range names {
			set[n] = true
		}
		return set
	}
	flatSet, idxSet := in(flat), in(idx)
	var flatOnly, idxOnly []string
	for _, n := range flat {
		if !idxSet[n] {
			flatOnly = append(flatOnly, n)
		}
	}
	for _, n := range idx {
		if !flatSet[n] {
			idxOnly = append(idxOnly, n)
		}
	}
	if len(flatOnly) > 0 || len(idxOnly) > 0 {
		t.Errorf("the flattener and the index split words at different names: flattener only %v, index only %v", flatOnly, idxOnly)
	}
	for _, n := range []string{"div", "p", "td", "dt", "dd", "pre"} {
		if !flatSet[n] || !idxSet[n] {
			t.Errorf("%q must split words on both sides (flattener %v, index %v)", n, flatSet[n], idxSet[n])
		}
	}
}
