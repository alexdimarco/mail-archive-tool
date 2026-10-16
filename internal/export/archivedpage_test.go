package export

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/model"
)

// covers: MA-420, MA-531, R19, R7, R29
// The archived-page gate (extraction design §4.11, RR2-C9): a page the current renderer wrote
// is inert (its own leading charset and policy metas are not "hostile", its own header
// classes are not reserved markers in it), and the gate refuses what only its prefix could
// not: a page with no policy prefix whatever it holds, a page with the right prefix and a
// live refresh after it (rendered before the F1 fix), a foreign policy or charset meta, a
// resource hint, one that hides its meta in a noscript, and a page the parser cannot read.
func TestArchivedPageGate(t *testing.T) {
	render := func(subject, body string) []byte {
		rr, err := RenderWith(&model.Message{Subject: subject, SenderEmail: "a@example.com", HTMLBody: body}, RenderContext{RawName: "x.eml", RootRel: "../", FolderIndexRel: "index.html"})
		if err != nil {
			t.Fatal(err)
		}
		return rr.HTML
	}
	current := render("a hello", `<p>Hello <b>world</b> <script>not live: the policy meta precedes everything</script></p>`)
	if !strings.HasPrefix(string(current), docPrefix) {
		t.Fatalf("the renderer's page does not start with the gate's prefix:\n%.200s", current)
	}
	// the positive twin first
	if LiveRefreshIn(current) {
		t.Fatalf("a page of the current renderer is reported to carry a live refresh:\n%s", current)
	}
	if !ArchivedPageInert(current) {
		t.Fatal("a page of the current renderer is not inert: it would be quarantined as text")
	}
	plain := render("plain", "")
	if !ArchivedPageInert(plain) {
		t.Error("a plain-text page of the current renderer is not inert")
	}

	// after the prefix: the first line after the policy metas is the viewport meta
	insertAfterPrefix := func(extra string) []byte {
		return []byte(strings.Replace(string(current), docPrefix, docPrefix+"\n"+extra, 1))
	}
	for name, page := range map[string][]byte{
		"a live refresh after the right prefix (a page of a renderer before the F1 fix)": insertAfterPrefix(`<meta http-equiv="refresh" content="0;url=http://evil.example/">`),
		"a refresh whose attribute is cased and spaced":                                  insertAfterPrefix(`<META HTTP-EQUIV=" Refresh " CONTENT="1">`),
		"a foreign policy meta": insertAfterPrefix(`<meta http-equiv="Content-Security-Policy" content="default-src *">`),
		"a competing charset":   insertAfterPrefix(`<meta charset="utf-7">`),
		"a content-type pragma": insertAfterPrefix(`<meta http-equiv="content-type" content="text/html; charset=utf-7">`),
		"a set-cookie pragma":   insertAfterPrefix(`<meta http-equiv="set-cookie" content="a=b">`),
		"a preconnect hint":     insertAfterPrefix(`<link rel="preconnect" href="http://evil.example/">`),
		"a refresh inside a noscript, which a viewer with scripts off reads as markup": insertAfterPrefix(`<noscript><meta http-equiv="refresh" content="0;url=http://evil.example/"></noscript>`),
	} {
		if !LiveRefreshIn(page) {
			t.Errorf("%s: not reported as live", name)
		}
		if ArchivedPageInert(page) {
			t.Errorf("%s: the gate would keep it as .html", name)
		}
	}

	// no prefix: a script and no policy meta before it, which the metas check alone passes
	scripted := []byte("<!DOCTYPE html><html><head><title>x</title></head><body><script>alert(document.cookie)</script></body></html>")
	if LiveRefreshIn(scripted) {
		t.Log("LiveRefreshIn reports metas only: the prefix is what refuses a scripted page")
	}
	if ArchivedPageInert(scripted) {
		t.Error("a page with a <script> and no policy prefix would keep its .html name")
	}
	if ArchivedPageInert([]byte(strings.TrimPrefix(string(current), "<!DOCTYPE html>\n"))) {
		t.Error("a page that lost one byte of the renderer's exact prefix would keep its .html name")
	}
	if ArchivedPageInert(nil) || ArchivedPageInert([]byte("hello")) {
		t.Error("a file that is not a rendered page is inert")
	}

	// a page the parser cannot read is quarantined, never trusted
	deep := string(current) + strings.Repeat("<div>", 600)
	if !LiveRefreshIn([]byte(deep)) || ArchivedPageInert([]byte(deep)) {
		t.Error("a page nested past the parser's cap is not refused")
	}
}
