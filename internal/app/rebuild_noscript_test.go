package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
)

// covers: MA-187, MA-492, R8, R13, R19, S33
// A hostile mail cannot erase its own record by hiding its archived page's
// header. The in-place noscript retry (d787013) wrote pages whose head
// <noscript> never closed — its rewritten content ended in a <plaintext>, after
// which x/net writes nothing, or in a script left in an escape state by
// `<!--<script>` — so the tool's header and the mail's body became raw text:
// the page showed neither, and `reindex -rebuild` (which anchors on the
// header div and the body div directly in <body>) found no subject, sender
// or date and no body words, while the live index had them all (the
// reviewer's nine, rv3/corpus-adv adv_plaintext_head_{esc,lnk,ref}_{a,d} and
// adv_scriptesc_head_{esc,lnk,ref}_a: live=1 rebuilt=0 for each sender,
// unrecovered-fields=18 on the 372-vector corpus). Two heads did the same with
// no retry to blame: a head <script> left in an escape state at the end of the
// mail (both readings) and a head <noscript> holding a <plaintext> (with
// scripting off); a head <noscript> holding a <frameset>, or an element left
// open around the header, did it with scripting off until the retry wrote the
// content closed. End to end, every page here keeps the header and the body
// div live directly in <body> in both readings, the rebuild recovers every
// record's own subject, sender and date (unrecovered 0), and each sender,
// subject word and body word has the same hits live and rebuilt.
func TestRebuildHostileHeadMarkupCannotEraseItsRecord(t *testing.T) {
	const (
		esc = `&lt;meta http-equiv="refresh" content="0;url=http://127.0.0.1:8799/b/adv"&gt;`
		lnk = `<link rel="preconnect" href="http://127.0.0.1:8799/b/adv">`
		ref = `<meta http-equiv="refresh" content="0;url=http://127.0.0.1:8799/b/adv">`
	)
	head := func(ns string) string {
		return `<html><head><noscript><meta charset=utf-8>` + ns + `</noscript></head><body><p>adv words</p></body></html>`
	}
	vectors := []struct{ word, body string }{
		{"hhplainesca", head(`<plaintext></noscript>` + esc)},
		{"hhplainescd", head(`<plaintext>` + esc)},
		{"hhplainlnka", head(`<plaintext></noscript>` + lnk)},
		{"hhplainlnkd", head(`<plaintext>` + lnk)},
		{"hhplainrefa", head(`<plaintext></noscript>` + ref)},
		{"hhplainrefd", head(`<plaintext>` + ref)},
		{"hhscriptesca", head(`<script><!--<script></noscript>` + esc + `</script>`)},
		{"hhscriptlnka", head(`<script><!--<script></noscript>` + lnk + `</script>`)},
		{"hhscriptrefa", head(`<script><!--<script></noscript>` + ref + `</script>`)},
		{"hhheadscript", `<html><head><title>t</title><script><!--<script></head><body><p>adv words</p></body></html>`},
		{"hhheadplain", `<html><head><noscript><plaintext>nsheadword</noscript></head><body><p>adv words</p></body></html>`},
		{"hhheadframes", `<html><head><noscript><frameset></noscript></head><body><p>adv words</p></body></html>`},
		{"hhheadopendiv", `<html><head><noscript><div style="display:none">enable scripts</noscript></head><body><p>adv words</p></body></html>`},
	}
	when := time.Date(2025, 4, 2, 10, 2, 0, 0, time.UTC)
	var msgs []rbMsg
	for i, v := range vectors {
		msgs = append(msgs, rbMsg{[]string{"Inbox"}, &model.Message{
			Subject: "hidden header " + v.word, SenderName: "Corpus Sender", SenderEmail: v.word + "@example.com",
			To: "rcpt@example.com", InternetMessageID: "<" + v.word + "@example.com>",
			Received: when.Add(time.Duration(i) * time.Minute), HTMLBody: v.body,
		}})
	}
	out := tmpDir(t)
	rels := buildRebuildArchive(t, out, msgs)

	// Every page keeps the renderer's anchor live in both readings: the
	// first header div directly in <body>, the body div the next element.
	for _, v := range vectors {
		rel := assure.Reached(t, rels["hidden header "+v.word], "page path of "+v.word)
		page, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		for _, scripting := range []bool{true, false} {
			root, err := xhtml.ParseWithOptions(bytes.NewReader(page), xhtml.ParseOptionEnableScripting(scripting))
			if err != nil {
				t.Fatalf("%s: the page does not parse (scripting=%v): %v", v.word, scripting, err)
			}
			bodyEl := findFirst(root, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "body" })
			var header, next *xhtml.Node
			if bodyEl != nil {
				header = firstChildDiv(bodyEl, "mailarchive-header")
			}
			if header != nil {
				next = nextElementSibling(header)
			}
			if header == nil || next == nil || next.Data != "div" || !hasClass(next, "mailarchive-body") {
				t.Errorf("%s (scripting=%v): the header div and the body div must be live directly in <body>", v.word, scripting)
			}
		}
	}

	queries := []string{"adv"}
	for _, v := range vectors {
		queries = append(queries, v.word, "from:"+v.word)
	}
	live := map[string]int{}
	for _, q := range queries {
		live[q] = searchTotal(t, out, q)
	}
	deleteSearchDB(t, out)
	if rep := rebuildOnDisk(t, out, len(vectors)); rep.Unrecovered != 0 {
		t.Errorf("rebuild unrecovered-fields = %d, want 0: a mail erased its own subject, sender or date", rep.Unrecovered)
	}
	for _, q := range queries {
		want := 1
		if q == "adv" {
			// Not in hhheadscript: its own parse puts "adv words" inside the
			// head script, so no reader of the mail sees them, and neither index
			// finds them.
			want = len(vectors) - 1
		}
		if rebuilt := searchTotal(t, out, q); live[q] != want || rebuilt != want {
			t.Errorf("search %q: live=%d rebuilt=%d, want %d on both", q, live[q], rebuilt, want)
		}
	}
	ix, err := index.OpenReadonly(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	for i, v := range vectors {
		res, _, err := ix.Search(index.ParseQuery("from:"+v.word, index.Query{}))
		if err != nil {
			t.Fatal(err)
		}
		if len(res) != 1 || res[0].Subject != "hidden header "+v.word || res[0].SenderEmail != v.word+"@example.com" ||
			!res[0].Date.Equal(when.Add(time.Duration(i)*time.Minute)) {
			t.Errorf("%s: the rebuilt record must carry its own subject, sender and date, got %+v", v.word, res)
		}
	}
}

// covers: MA-492, R8, R13
// A body <noscript> whose in-place rewrite would not read back as the mail
// read is refused, and the words around it are found the same live and
// rebuilt. The rewritten content is written by x/net's Render, and in the
// scripting-on reading (the browser's default, and the rebuild's) it must be
// raw text closed by the noscript's own end tag: a <plaintext> is never
// closed (it swallowed the rest of the mail), text that decodes to
// `</noscript><plaintext>` or `</noscript></div>` ends it early (the `</div>`
// closed the page's body div, so the rebuild lost every word after it), a
// noscript nested in a <div> in the content leaves that `</div>` after the
// outer noscript's raw text ends (the same), and under an SVG foreignObject
// inside an SVG <noscript> a nested noscript's leftover end tag is read by the
// foreign-content rules as the SVG noscript's, so the HTML <xmp> after it
// became an SVG xmp holding an SVG <style> — its word was found live only. A
// refused noscript keeps its raw text: beside a charset meta that sends the
// mail to the source fallback (indexed by its words on both sides), and beside
// an accepted rewrite of another noscript the page renders with the refused
// one's words read from its raw text on both sides. Each word is found once
// live and once rebuilt.
func TestLiveAndRebuiltIndexAgreeOnRefusedNoscriptRewrites(t *testing.T) {
	bodies := map[string]string{
		"rf plaintext":       `<p>rfplainbefore</p><noscript><meta charset=utf-8><plaintext>rfplainns</noscript><p>rfplainafter</p>`,
		"rf script escape":   `<p>rfscrbefore</p><noscript><meta charset=utf-8><script><!--<script></noscript><p>rfscrafter</p>`,
		"rf decoded plain":   `<p>rfdecbefore</p><noscript>rfdeca&lt;/noscript&gt;&lt;plaintext&gt;rfdecb<meta charset=utf-8></noscript><p>rfdecafter</p>`,
		"rf decoded div end": `<p>rfdivbefore</p><noscript>rfdivns&lt;/noscript&gt;&lt;/div&gt;<meta charset=utf-8></noscript><p>rfdivafter</p>`,
		"rf nested in a div": `<p>rfnestbefore</p><noscript><meta charset=utf-8><div><noscript>rfnestinner</div>rfnestouter</noscript><p>rfnestafter</p>`,
		"rf svg stray":       `<p>rfsvgbefore</p><svg><noscript><foreignObject><noscript><meta charset=utf-8><noscript>rfsvginner</noscript>rfsvgmore <xmp>q<style>rfsvghid</style>z</xmp></foreignObject></noscript></svg><p>rfsvgafter</p>`,
		"rf refused beside":  `<p>rfm5before</p><noscript><meta charset=utf-8></noscript><noscript><div><noscript>rfm5inner</div>rfm5outer</noscript><p>rfm5after</p>`,
	}
	want := map[string]int{
		"rfplainbefore": 1, "rfplainns": 1, "rfplainafter": 1,
		"rfscrbefore": 1, "rfscrafter": 1,
		"rfdecbefore": 1, "rfdeca": 1, "rfdecb": 1, "rfdecafter": 1,
		"rfdivbefore": 1, "rfdivns": 1, "rfdivafter": 1,
		"rfnestbefore": 1, "rfnestinner": 1, "rfnestouter": 1, "rfnestafter": 1,
		"rfsvgbefore": 1, "rfsvginner": 1, "rfsvgmore": 1, "rfsvghid": 1, "rfsvgafter": 1,
		"rfm5before": 1, "rfm5inner": 1, "rfm5outer": 1, "rfm5after": 1,
	}
	live, rebuilt := parityArchive(t, bodies, keysOf(want))
	assure.Reached(t, live["rfnestafter"]+rebuilt["rfnestafter"], "hits for a word after a refused noscript")
	for _, p := range parityProblems(want, live, rebuilt) {
		t.Error(p)
	}
}
