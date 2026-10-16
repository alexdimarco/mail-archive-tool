package export

import (
	"testing"

	"mail-archive-tool/internal/model"
)

// covers: MA-80, MA-474, R7, R19
// R3-AC3 / NF-1 (round-3 re-verification). The page check compares the page's
// reading of div.mailarchive-body with the sanitized markup byte for byte. The
// sanitizer parsed the mail in the MAIL's compatibility mode and the page is
// standards mode, so ordinary old markup serialized differently and read as
// hostile: in quirks mode a <table> inside an open <p> stays in the <p>, in
// standards mode it closes the <p> (and leaves a stray empty </p>); and
// `<form><b>x</form><form>y</form>` nests the second form in the re-opened <b>
// in one parse and not in the other. Such mail (no doctype, an HTML 4.01
// Transitional doctype, a FrontPage or Thunderbird page) rendered before the
// check and must render as HTML now: no fallback, its words shown in the body
// div in both scripting readings.
func TestRenderBenignQuirksModeMarkupStaysHTML(t *testing.T) {
	cases := map[string]string{
		"no doctype p table":    `<p>nfonehello<table><tr><td>nfonecell</td></tr></table>nfoneworld</p>`,
		"html body p table":     `<html><body><p>nftwohello<table><tr><td>nftwocell</td></tr></table>nftwoworld</p></body></html>`,
		"transitional P TABLE":  `<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN"><HTML><BODY><P align=center>nfthreehead<TABLE><TR><TD>nfthreecell</TD></TR></TABLE></BODY></HTML>`,
		"div p table":           `<div><p>nffourtop<table><tr><td>nffourcell</td></tr></table></div>`,
		"thunderbird layout":    `<html><head><meta http-equiv="content-type" content="text/html; charset=utf-8"></head><body text="#000000" bgcolor="#FFFFFF"><p>nffivehi<table border="1"><tr><td>nffivecell</td></tr></table>nffiveend</p></body></html>`,
		"form with open b":      `<form><b>nfsixone</form><form>nfsixtwo</form>`,
		"form open b in table":  `<table><tr><td><form><b>nfsevenone</form><form>nfseventwo</form></td></tr></table>`,
		"html5 doctype control": `<!DOCTYPE html><p>nfeightone<table><tr><td>nfeightcell</td></tr></table>nfeightend</p>`,
	}
	words := map[string][]string{
		"no doctype p table":    {"nfonehello", "nfonecell", "nfoneworld"},
		"html body p table":     {"nftwohello", "nftwocell", "nftwoworld"},
		"transitional P TABLE":  {"nfthreehead", "nfthreecell"},
		"div p table":           {"nffourtop", "nffourcell"},
		"thunderbird layout":    {"nffivehi", "nffivecell", "nffiveend"},
		"form with open b":      {"nfsixone", "nfsixtwo"},
		"form open b in table":  {"nfsevenone", "nfseventwo"},
		"html5 doctype control": {"nfeightone", "nfeightcell", "nfeightend"},
	}
	for name, body := range cases {
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: body}, RenderContext{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out.BodyFallback != "" {
			t.Errorf("%s: ordinary markup must render as HTML, got fallback %q", name, out.BodyFallback)
			continue
		}
		for i, root := range pageTrees(t, name, string(out.HTML)) {
			_, b := pageLayout(root)
			if b == nil {
				t.Errorf("%s: reading %d: no body div", name, i)
				continue
			}
			for _, w := range words[name] {
				if !shownWords(b, false, w) {
					t.Errorf("%s: reading %d: %q must be shown in the body div", name, i, w)
				}
			}
		}
	}
}
