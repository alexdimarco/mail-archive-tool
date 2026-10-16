package source

import (
	"strings"
	"testing"
)

// covers: MA-333, MA-340, R1, R7, R26
// MS2-AO7. A label the charset table maps to the replacement encoding (iso-2022-kr,
// csiso2022kr, iso-2022-cn: WHATWG decodes these to a single U+FFFD) is a label this
// version cannot decode, so it is read as every other label it does not know: shown as
// UTF-8 if it is one, else as Windows-1252, with a charset-fallback note. The text of a
// message does not vanish from the archive page and the index with no sign (design 4.4, P7).
func TestAReplacementEncodingLabelDoesNotTurnTheBodyIntoOneReplacementCharacter(t *testing.T) {
	useFidelity(t)
	const text = "Wire the payment to account 12345 today."
	for _, label := range []string{"iso-2022-kr", "csiso2022kr", "iso-2022-cn", "ISO-2022-KR"} {
		m := safeParseMessage(hostile("text/plain; charset="+label, text+"\r\n"))
		requireNoStub(t, label, m)
		if !strings.Contains(m.PlainBody, text) || strings.Contains(m.PlainBody, "�") {
			t.Errorf("charset %s: the body is %q, want the text as sent", label, m.PlainBody)
		}
		if !hasNote(m, noteCharsetFallback) {
			t.Errorf("charset %s: notes %v, want a charset-fallback note naming the label", label, noteCodes(m))
		}
	}

	// The twins: a label that decodes (hz-gb-2312 and iso-8859-1 are in the table) is decoded
	// with it and carries no note.
	for _, label := range []string{"hz-gb-2312", "iso-8859-1", "utf-8"} {
		m := safeParseMessage(hostile("text/plain; charset="+label, text+"\r\n"))
		if !strings.Contains(m.PlainBody, text) || hasNote(m, noteCharsetFallback) {
			t.Errorf("charset %s: body %q notes %v, want the text and no charset-fallback note", label, m.PlainBody, noteCodes(m))
		}
	}
	// An empty part in such a charset stays empty: nothing was lost, so nothing is noted.
	if m := safeParseMessage(hostile("text/plain; charset=iso-2022-kr", "")); hasNote(m, noteCharsetFallback) {
		t.Errorf("an empty body: %q %v", m.PlainBody, noteCodes(m))
	}
}
