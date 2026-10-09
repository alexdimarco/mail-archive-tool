package app

import (
	"fmt"
	"io"
	"log"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// covers: MA-492, R8, R13, S33
// The live index (BodyText on the original mail) and `reindex -rebuild`
// (BodyText on the archived page's body div) read the same words. RV-3: the
// SVG style/comment trick (adv2-scripts/mkhide.py) hid the words after it from
// both, though the page shows them. RV-4: a mail without `</head>`
// (adv2-scripts/mknohead.py) had no body words in the live index, but did after
// a rebuild. Each word is now found once, on the live index and after a
// rebuild, at ordinary depth and nested past the tree parser's cap (a page
// shown as source, read by the fallback extractor on both sides). So are the
// words after an SVG that a stray `</p>` or `</br>` closes (adv2 rv5/mkbr.py):
// the parser leaves foreign content there, so the <style> that follows is HTML
// and the comment trick hides nothing; the fallback once missed that. Positive
// twins: an ordinary mail's title, style and script words are found on neither
// side, nor are a noscript's tag and attribute words, while its text is; a
// noscript in the mail's explicit <head> (which the page keeps in its own head,
// out of the rebuild's reach) is found on neither side.
func TestLiveAndRebuiltIndexReadTheSameVisibleWords(t *testing.T) {
	out := tmpDir(t)
	date := time.Date(2025, 4, 2, 12, 30, 0, 0, time.UTC)
	n := 0
	mk := func(subject, body string) rbMsg {
		n++
		return rbMsg{[]string{"Inbox"}, &model.Message{
			Subject: subject, SenderName: "Parity Sender", SenderEmail: "parity@example.com",
			InternetMessageID: "<parity-" + strconv.Itoa(n) + "@example.com>",
			Received:          date.Add(time.Duration(n) * time.Minute),
			HTMLBody:          body,
		}}
	}
	deep := func(inner string) string {
		return strings.Repeat("<div>", 600) + inner + strings.Repeat("</div>", 600)
	}
	buildRebuildArchive(t, out, []rbMsg{
		mk("quarterly update", `<html><body><p>Hello team</p><svg width="0" height="0"><style><!--</style><script>--></style></svg><p>please wire funds to account kestreleleven today</p></body></html>`),
		mk("skeptic hide", `<p>Hello skepvisible</p><svg width=0 height=0><style><!--</style><script>--></style></svg><p>please send to account skephidden today</p>`),
		mk("nohead ordinary", `<html><head><meta name="viewport" content="width=device-width"><body><p>hello kestreltwelve</p></body></html>`),
		mk("withhead ordinary", `<html><head><meta name="viewport" content="width=device-width"></head><body><p>hello kestrelthirteen</p></body></html>`),
		mk("nohead title", `<head><title>t</title><body><p>hello skepnoheadtitleword</p>`),
		mk("ordinary twin", `<html><head><title>parititleword</title><style>.x{} paristyleword</style><script>pariscriptword()</script></head>`+
			`<body><p>paribodyword</p><style>paribodystyle</style><script>paribodyscript</script>`+
			`<noscript><p>parinoscriptword</p><img alt="parinsaltword"></noscript></body></html>`),
		mk("head noscript", `<html><head><noscript><p>parheadnsword</p></noscript></head><body><p>parheadnsbody</p></body></html>`),
		mk("deep hide", `<html><body>`+deep(`<p>Hello</p><svg><style><!--</style><script>--></style></svg><p>account deepkestreleleven today</p>`)+`</body></html>`),
		mk("deep nohead", `<html><head><meta name="viewport" content="x"><body>`+deep(`<p>hello deepkestreltwelve</p>`)+`</body></html>`),
		mk("deep p breakout", `<html><body>`+deep(`<p>Hello brvisibleone</p><svg width=0 height=0></p><style><!--</style><p>please wire funds to brhiddenp today</p>--></style></svg><p>brtailone</p>`)+`</body></html>`),
		mk("deep br breakout", `<html><body>`+deep(`<p>Hello brvisibletwo</p><svg width=0 height=0></br><style><!--</style><p>please wire funds to brhiddenbr today</p>--></style></svg><p>brtailtwo</p>`)+`</body></html>`),
		mk("shallow p breakout", `<html><body><p>Hello</p><svg width=0 height=0></p><style><!--</style><p>please wire funds to brshallowp today</p>--></style></svg></body></html>`),
		mk("shallow br breakout", `<html><body><p>Hello</p><svg width=0 height=0></br><style><!--</style><p>please wire funds to brshallowbr today</p>--></style></svg></body></html>`),
	})

	want := map[string]int{
		"kestreleleven": 1, "wire": 5, "skephidden": 1, "skepvisible": 1,
		"kestreltwelve": 1, "kestrelthirteen": 1, "skepnoheadtitleword": 1,
		"paribodyword": 1, "parinoscriptword": 1, "parheadnsbody": 1, "parheadnsword": 0,
		"parititleword": 0, "paristyleword": 0, "pariscriptword": 0, "paribodystyle": 0, "paribodyscript": 0, "parinsaltword": 0,
		"deepkestreleleven": 1, "deepkestreltwelve": 1,
		"brvisibleone": 1, "brhiddenp": 1, "brtailone": 1, "brvisibletwo": 1, "brhiddenbr": 1, "brtailtwo": 1,
		"brshallowp": 1, "brshallowbr": 1,
	}
	live := map[string]int{}
	for q := range want {
		live[q] = indexTotal(t, out, q)
	}

	deleteSearchDB(t, out)
	rep, err := Rebuild(out, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if rep.Rebuilt != 13 {
		t.Fatalf("rebuild report = %+v, want Rebuilt=13 (every page re-derived)", rep)
	}
	for q, w := range want {
		rebuilt := indexTotal(t, out, q)
		if live[q] != w || rebuilt != w {
			t.Errorf("search %q: live=%d rebuilt=%d, want %d on both", q, live[q], rebuilt, w)
		}
	}
	r := firstResult(t, out, "kestreltwelve")
	if r.Subject != "nohead ordinary" || !strings.Contains(r.Snippet, "kestreltwelve") {
		t.Errorf("rebuilt nohead record: subject=%q snippet=%q", r.Subject, r.Snippet)
	}
}

// parityArchive exports bodies as HTML mails (the live index reads each
// original mail), then deletes search.db and rebuilds it from the pages. It
// returns each query's hit count on both sides.
func parityArchive(t *testing.T, bodies map[string]string, queries []string) (live, rebuilt map[string]int) {
	t.Helper()
	out := tmpDir(t)
	date := time.Date(2025, 4, 3, 9, 0, 0, 0, time.UTC)
	var msgs []rbMsg
	n := 0
	for subject, body := range bodies {
		n++
		msgs = append(msgs, rbMsg{[]string{"Inbox"}, &model.Message{
			Subject: subject, SenderName: "Parity Sender", SenderEmail: "parity@example.com",
			InternetMessageID: "<parity2-" + strconv.Itoa(n) + "@example.com>",
			Received:          date.Add(time.Duration(n) * time.Minute),
			HTMLBody:          body,
		}})
	}
	buildRebuildArchive(t, out, msgs)
	live = map[string]int{}
	for _, q := range queries {
		live[q] = indexTotal(t, out, q)
	}
	deleteSearchDB(t, out)
	rep, err := Rebuild(out, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if rep.Rebuilt != len(bodies) {
		t.Fatalf("rebuild report = %+v, want Rebuilt=%d (every page re-derived)", rep, len(bodies))
	}
	rebuilt = map[string]int{}
	for _, q := range queries {
		rebuilt[q] = indexTotal(t, out, q)
	}
	return live, rebuilt
}

// parityProblems lists each query whose count is not the wanted one, on the
// live index or after the rebuild.
func parityProblems(want map[string]int, live, rebuilt map[string]int) []string {
	var problems []string
	for q, w := range want {
		if live[q] != w || rebuilt[q] != w {
			problems = append(problems, fmt.Sprintf("search %q: live=%d rebuilt=%d, want %d on both", q, live[q], rebuilt[q], w))
		}
	}
	sort.Strings(problems)
	return problems
}

func keysOf(m map[string]int) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// covers: MA-492, R8, R13
// explicitHead decides which head is kept out of the live index, and the
// decision must match what the rebuild can read. A head written the usual
// way, on its own line after <html> (and after a doctype and a comment, as
// Litmus-style templates have it), is still explicit: the exporter keeps it in
// the page's own <head>, so its noscript text is found on neither side. A
// mail whose explicit <body> starts with a noscript keeps that noscript at
// the start of the page's body div, where the rebuild's own parse puts it in
// an implied head; implied heads are read, so its text is found on both sides.
func TestLiveAndRebuiltIndexAgreeOnHeadsAroundNoscript(t *testing.T) {
	bodies := map[string]string{
		"newline head": "<html>\n<head>\n<noscript><p>parnlheadns</p></noscript>\n</head>\n<body>\n<p>parnlbody</p>\n</body>\n</html>",
		"litmus head": "<!DOCTYPE html>\n<!-- preheader -->\n<html lang=\"en\" xmlns=\"http://www.w3.org/1999/xhtml\">\n  <head>\n    <meta charset=\"utf-8\">\n" +
			"    <noscript><p>parlitheadns</p></noscript>\n  </head>\n  <body>\n    <p>parlitbody</p>\n  </body>\n</html>",
		"body starts with noscript": `<html><head><title>t</title></head><body><noscript><p>parbodystartns</p></noscript><p>parbodystartbody</p></body></html>`,
	}
	want := map[string]int{
		"parnlheadns": 0, "parnlbody": 1,
		"parlitheadns": 0, "parlitbody": 1,
		"parbodystartns": 1, "parbodystartbody": 1,
	}
	live, rebuilt := parityArchive(t, bodies, keysOf(want))
	assure.Reached(t, live["parnlbody"]+rebuilt["parnlbody"], "hits for an indexed body word")
	for _, p := range parityProblems(want, live, rebuilt) {
		t.Error(p)
	}
}

// covers: MA-492, R8, R13
// A head <noscript> that holds a charset <meta> and text (adv2 rv5 corpus
// 46_head_noscript_charset_text): with scripting on, the parser keeps the
// noscript's content in the head as raw text; with scripting off, the <p>
// leaves the head and becomes body content. Which of the two the page shows
// is the exporter's choice: today the dual-mode re-check sends this mail to
// the source fallback, and a scripting-off retry would render the text in
// the body. Whatever it shows, the live index and the rebuilt one must find
// the same words. So this pins parity, not a count, for the noscript text,
// and the body word is found once on both sides.
func TestLiveAndRebuiltIndexAgreeOnHeadNoscriptMarkup(t *testing.T) {
	bodies := map[string]string{
		"head noscript charset": `<html><head><noscript><meta charset="utf-8"><p>parheadnstext</p></noscript></head><body><p>parheadnsbodyword</p></body></html>`,
	}
	same := []string{"parheadnstext"}
	want := map[string]int{"parheadnsbodyword": 1}
	live, rebuilt := parityArchive(t, bodies, append(keysOf(want), same...))
	assure.Reached(t, live["parheadnsbodyword"]+rebuilt["parheadnsbodyword"], "hits for the body word")
	for _, p := range parityProblems(want, live, rebuilt) {
		t.Error(p)
	}
	for _, q := range same {
		if live[q] != rebuilt[q] {
			t.Errorf("search %q: live=%d rebuilt=%d, want the same count on both (R8)", q, live[q], rebuilt[q])
		}
	}
}
