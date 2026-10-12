package source

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-338, R1, R10, R26
// C1-4: a malformed Content-Type parameter does not lose the multipart boundary, the body
// or the attachment names. An unquoted "=" boundary, an unquoted start=<x@y> and type=
// text/html, a charset with a trailing comment and an unquoted filename with a space are
// each recovered and noted lenient-params. A multipart with no recoverable boundary keeps
// its body as text with an unsplit-multipart gap (fillable on an on-demand source,
// terminal on Graph, MC20), never a "missing body". Twin: strict-valid headers read as
// before, with no note.
func TestMalformedContentTypeParametersAreRecovered(t *testing.T) {
	useFidelity(t)
	pdf := b64(t, corpusPDFBase64)

	m := safeParseMessage(corpus(t, "c14-unquoted-boundary-eq.eml"))
	if a, ok := attNamed(m, "r.pdf"); !ok || !bytes.Equal(bytesOf(t, a), pdf) || !strings.Contains(m.PlainBody, "The body of an Outlook-style message.") {
		t.Errorf("unquoted boundary with '=': attachments %v body %q", attNames(m), m.PlainBody)
	}
	if !hasNote(m, noteLenientParams) {
		t.Errorf("unquoted boundary: notes %v lack lenient-params", noteCodes(m))
	}

	m = safeParseMessage(corpus(t, "c14-unquoted-start-type.eml"))
	if !strings.Contains(m.HTMLBody, "<p>Related html</p>") {
		t.Errorf("unquoted start and type: html body %q", m.HTMLBody)
	}
	// (An image with no name is still named by v1's rule: its Label is cid:<id>; naming is S3's.)
	if len(m.Attachments) != 1 || m.Attachments[0].ContentID != "img1@corpus.example" || m.Attachments[0].MimeType != "image/png" {
		t.Errorf("unquoted start and type: attachments %+v", m.Attachments)
	}
	if !hasNote(m, noteLenientParams) {
		t.Errorf("unquoted start and type: notes %v lack lenient-params", noteCodes(m))
	}

	m = safeParseMessage(corpus(t, "c14-charset-comment.eml"))
	if !strings.Contains(m.PlainBody, "The charset parameter carries a trailing comment.") || !hasNote(m, noteLenientParams) {
		t.Errorf("charset with a comment: body %q notes %v", m.PlainBody, noteCodes(m))
	}

	m = safeParseMessage(corpus(t, "c14-unquoted-filename.eml"))
	if a, ok := attNamed(m, "Invoice 2023.pdf"); !ok || !bytes.Equal(bytesOf(t, a), pdf) {
		t.Errorf("unquoted filename: attachments %v (v1 named it attachment-N)", attNames(m))
	}
	if !strings.Contains(m.PlainBody, "An attachment with an unquoted filename follows.") || !hasNote(m, noteLenientParams) {
		t.Errorf("unquoted filename: body %q notes %v", m.PlainBody, noteCodes(m))
	}

	// No boundary to find: the body is kept as text, with a gap that is not "missing body".
	m = safeParseMessage(corpus(t, "c14-no-boundary.eml"))
	if !strings.Contains(m.PlainBody, "The body, with no boundary parameter to find it.") {
		t.Errorf("no boundary: body %q", m.PlainBody)
	}
	if len(m.ParseGaps) != 1 || m.ParseGaps[0].Kind != state.GapKindUnsplitMultipart || m.ParseGaps[0].Detail == "" {
		t.Fatalf("no boundary: gaps %v, want one unsplit-multipart", m.ParseGaps)
	}
	label := state.ParseGapLabel(m.ParseGaps[0].Kind, m.ParseGaps[0].Detail)
	if state.RecordGapClass(label, false) != state.ClassFillable || state.RecordGapClass(label, true) != state.ClassTerminal {
		t.Errorf("unsplit-multipart is %q in Missing and %q in Terminal, want fillable and terminal", state.RecordGapClass(label, false), state.RecordGapClass(label, true))
	}

	// The twin: strict-valid headers are read as before, with no note and no gap.
	m = safeParseMessage(corpus(t, "c14-strict-valid.eml"))
	if hasNote(m, noteLenientParams) || len(m.ParseGaps) != 0 || m.FellBack {
		t.Errorf("strict twin: notes %v gaps %v", noteCodes(m), m.ParseGaps)
	}

	// v1 lost the boundary, and with it every part, in each shape.
	for _, file := range []string{"c14-unquoted-boundary-eq.eml", "c14-unquoted-start-type.eml"} {
		if v1 := corpusV1Parse(corpus(t, file)); v1.PlainBody != "" || v1.HTMLBody != "" || len(v1.Attachments) != 0 {
			t.Errorf("%s: v1 read the message (%q, %q, %d attachments); the test no longer shows the defect", file, v1.PlainBody, v1.HTMLBody, len(v1.Attachments))
		}
	}
}

// covers: MA-338, MA-349, R10, R26
// The lenient tokenizer's rules: the first segment is the type, names are lower-cased,
// a quoted value runs to its closing quote (";" and escapes inside it), an unquoted one
// to the next ";" with "=" and spaces and loses a trailing comment, RFC 2231 values and
// continuations join in index order and decode, the first occurrence of a name wins, at
// most 64 parameters are read, and a continuation index is only an ordering key.
func TestLenientTokenizerRules(t *testing.T) {
	for _, c := range []struct {
		name, in string
		mt       string
		want     map[string]string
	}{
		{"equals in an unquoted boundary", "multipart/mixed; boundary=----=_NextPart_000_0001_01D9", "multipart/mixed",
			map[string]string{"boundary": "----=_NextPart_000_0001_01D9"}},
		{"start and type", "Multipart/Related; type=text/html; start=<body@x.example>; boundary=rel-bd", "multipart/related",
			map[string]string{"type": "text/html", "start": "<body@x.example>", "boundary": "rel-bd"}},
		{"a trailing comment", "text/plain; charset=us-ascii (Plain text)", "text/plain", map[string]string{"charset": "us-ascii"}},
		{"a space in an unquoted value", "application/pdf; name=Invoice 2023.pdf", "application/pdf", map[string]string{"name": "Invoice 2023.pdf"}},
		{"a quoted semicolon and escape", `application/x; name="a;b\"c.txt"; boundary=x`, "application/x",
			map[string]string{"name": `a;b"c.txt`, "boundary": "x"}},
		{"names are lower-cased", "text/plain; CharSet=utf-8; BOUNDARY=B", "text/plain", map[string]string{"charset": "utf-8", "boundary": "B"}},
		{"the first occurrence wins", "x/y; a=1; a=2", "x/y", map[string]string{"a": "1"}},
		{"an extended value", "attachment; filename*=utf-8''caf%C3%A9.txt; x=bad (c)", "attachment", map[string]string{"filename": "café.txt", "x": "bad"}},
		{"continuations in index order", "attachment; filename*1=.txt; filename*0*=utf-8''caf%C3%A9; z=1", "attachment",
			map[string]string{"filename": "café.txt", "z": "1"}},
		{"a bad percent escape stays", "attachment; filename*=utf-8''a%zzb", "attachment", map[string]string{"filename": "a%zzb"}},
		{"a continuation index far past the bound is dropped", "attachment; filename*4294967295*=utf-8''x; n=1", "attachment", map[string]string{"n": "1"}},
		{"a segment with no equals sign is skipped", "text/plain; charset; format=flowed", "text/plain", map[string]string{"format": "flowed"}},
		{"nothing", "", "", map[string]string{}},
		{"only separators", ";;;", "", map[string]string{}},
	} {
		mt, params := lenientMediaType(c.in)
		if mt != c.mt || fmt.Sprint(params) != fmt.Sprint(c.want) {
			t.Errorf("%s: lenientMediaType(%q) = %q %v, want %q %v", c.name, c.in, mt, params, c.mt, c.want)
		}
	}

	// At most 64 parameters, the boundary among the first.
	var b strings.Builder
	b.WriteString("multipart/mixed; boundary=bd")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "; p%d=v%d", i, i)
	}
	_, params := lenientMediaType(b.String())
	if len(params) > maxLenientParams || params["boundary"] != "bd" {
		t.Errorf("%d parameters read from 200 (want at most %d), boundary %q", len(params), maxLenientParams, params["boundary"])
	}
}
