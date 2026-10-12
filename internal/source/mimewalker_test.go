package source

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/quotedprintable"
	"strings"
	"testing"
	"unicode/utf8"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/model"
)

// Tests of the fidelity walker (design-mime-fidelity §4.3, slice S2). The walker is the
// content parser once fidelityContent is on; these tests switch it on for their own
// duration and go through safeParseMessage, the routing production uses.

// useFidelity turns the S2 content on for the test and restores it afterwards.
func useFidelity(t *testing.T) {
	t.Helper()
	old := fidelityContent
	fidelityContent = true
	t.Cleanup(func() { fidelityContent = old })
}

var corpusCache map[string][]byte

// corpus returns one item of the fidelity corpus.
func corpus(t *testing.T, name string) []byte {
	t.Helper()
	if corpusCache == nil {
		corpusCache = map[string][]byte{}
		for _, it := range loadMimeCorpus(t) {
			corpusCache[it.name] = it.data
		}
	}
	b, ok := corpusCache[name]
	if !ok {
		t.Fatalf("the corpus has no %s", name)
	}
	return b
}

// bytesOf is an attachment's bytes.
func bytesOf(t *testing.T, a model.Attachment) []byte {
	t.Helper()
	if a.WriteTo == nil {
		t.Fatalf("attachment %q has no bytes", a.Filename)
	}
	var buf bytes.Buffer
	if _, err := a.WriteTo(&buf); err != nil {
		t.Fatalf("attachment %q: %v", a.Filename, err)
	}
	return buf.Bytes()
}

// attNamed is the attachment called name.
func attNamed(m *model.Message, name string) (model.Attachment, bool) {
	for _, a := range m.Attachments {
		if a.Filename == name {
			return a, true
		}
	}
	return model.Attachment{}, false
}

func attNames(m *model.Message) []string {
	var out []string
	for _, a := range m.Attachments {
		out = append(out, a.Filename)
	}
	return out
}

func hasNote(m *model.Message, code string) bool {
	for _, n := range m.ParseNotes {
		if n.Code == code {
			return true
		}
	}
	return false
}

func noteCodes(m *model.Message) []string {
	var out []string
	for _, n := range m.ParseNotes {
		out = append(out, n.Code)
	}
	return out
}

func b64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(s, "\n", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func qp(t *testing.T, s string) []byte {
	t.Helper()
	b, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(s)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The PDF every F24 item carries as its last part.
const corpusPDFBase64 = "JVBERi0xLjQKJeLjz9MgbWltZS1jb3JwdXMgci5wZGYKMSAwIG9iajw8L1R5cGUvQ2F0YWxvZz4+" +
	"ZW5kb2JqCnRyYWlsZXI8PC9Sb290IDEgMCBSPj4KJSVFT0YK"

// contentBlocks splits the content golden into its per-item blocks, by item name.
func contentBlocks(t *testing.T) map[string]string {
	t.Helper()
	body, _, err := goldenBody(mimeGoldenContent)
	if err != nil {
		t.Fatal(err)
	}
	blocks := map[string]string{}
	for _, b := range strings.Split(body, "\n== ")[0:] {
		b = strings.TrimPrefix(b, "== ")
		name, _, _ := strings.Cut(b, "\n")
		blocks[name] = "== " + b
		if !strings.HasSuffix(blocks[name], "\n") {
			blocks[name] += "\n"
		}
	}
	return blocks
}

// covers: MA-332, R2, R7, R27
// A corpus message with no fidelity note has the content golden's fields, renders as the
// current renderer renders the v1 message, and carries IdentityTerms equal to its content
// fields; it is also the guard that catches a note nobody registered (MC24): a message
// whose content moved has to carry a note saying so.
func TestNoNoteMessagesKeepTheContentGolden(t *testing.T) {
	useFidelity(t)
	items := loadMimeCorpus(t)
	golden := contentBlocks(t)
	var quiet, noted int
	for _, it := range items {
		m := safeParseMessage(it.data)
		if len(m.ParseNotes) > 0 || len(m.ParseGaps) > 0 {
			noted++
			continue
		}
		quiet++
		if m.FellBack {
			t.Errorf("%s: fell back with no note", it.name)
		}

		var sb strings.Builder
		writeContentGolden(&sb, it.name, m, false)
		if want := golden[it.name]; sb.String() != want {
			t.Errorf("%s carries no note but its content is not the golden's, at %s", it.name, firstDiff(want, sb.String()))
		}

		v1 := corpusV1Parse(it.data)
		ctx := export.RenderContext{RootRel: "../", FolderIndexRel: "index.html"}
		got, err1 := export.RenderWith(m, ctx)
		want, err2 := export.RenderWith(v1, ctx)
		if err1 != nil || err2 != nil {
			t.Fatalf("%s: render: %v / %v", it.name, err1, err2)
		}
		if !bytes.Equal(got.HTML, want.HTML) {
			t.Errorf("%s carries no note but renders differently from the v1 message", it.name)
		}

		terms := identityTermsOf(m)
		if m.IdentityTerms == nil || terms.Subject != m.IdentityTerms.Subject || terms.SenderEmail != m.IdentityTerms.SenderEmail ||
			terms.To != m.IdentityTerms.To || terms.Cc != m.IdentityTerms.Cc || terms.Bcc != m.IdentityTerms.Bcc ||
			terms.InternetMessageID != m.IdentityTerms.InternetMessageID || !terms.IdentityDate.Equal(m.IdentityTerms.IdentityDate) ||
			terms.HTMLBody != m.IdentityTerms.HTMLBody || terms.PlainBody != m.IdentityTerms.PlainBody || terms.RTFBody != m.IdentityTerms.RTFBody ||
			len(terms.Attachments) != len(m.IdentityTerms.Attachments) {
			t.Errorf("%s carries no note but its identity terms are not its content fields", it.name)
		}
	}
	// The positive twin: both kinds exist, so the guard checks something.
	t.Logf("%d corpus messages carry no note, %d carry one", quiet, noted)
	if quiet < 50 || noted < 25 {
		t.Errorf("%d corpus messages carry no note and %d carry one; the guard proves nothing unless both are common", quiet, noted)
	}
}

// covers: MA-330, R3, R2, R27, S49
// With the fidelity content on, the identity golden holds through the production routing:
// every corpus message's Identity, Fingerprint, ContentDigest and local and live keys are
// the frozen v1 replay's, whatever the walker made of the content, and the stub for a
// message v1 panics on keeps its identity.
func TestFidelityContentKeepsTheIdentityGolden(t *testing.T) {
	useFidelity(t)
	wantID, _, err := goldenBody(mimeGoldenIdentity)
	if err != nil {
		t.Fatal(err)
	}
	// The stub is what is left when v1 and the walker both panic (v1 alone leaves the
	// walker's content under the stub's identity, TestIdentityStubGap).
	restore := injectPanics(t, true, true)
	stub := safeParseMessage([]byte("any bytes"))
	restore()
	if stub.IdentityTerms == nil || stub.Subject != stubSubject {
		t.Fatalf("v1 and the walker both panicking is not today's stub: %+v", stub)
	}
	id, _ := renderGoldens(t, loadMimeCorpus(t), safeParseMessage, stub)
	if id != wantID {
		t.Errorf("the identity golden does not hold with the fidelity content on, at %s", firstDiff(wantID, id))
	}
}

// covers: MA-333, R1, R10, R26
// F24 charsets. Each label is tested both first and after the body. Every later part
// (incl. r.pdf) is archived, the text is decoded (valid UTF-8, Windows-1252 where the
// label is not known) and no gap is recorded. A top-level unknown-8bit message gets a
// decoded Subject and body (no fallbackParse). Twin: iso-8859-1. The page half of the row
// (a charset-fallback note on the page) is TestUnknownCharsetNoteShowsOnThePage (MF-S4).
//
// A second text part after the body is kept by v1's first-of-each rule, which assembly
// (slice S3) replaces: here the body is the first part and the later parts are archived.
func TestUnknownCharsetsKeepLaterPartsAndDecode(t *testing.T) {
	useFidelity(t)
	pdf := b64(t, corpusPDFBase64)
	const want = "Grüße aus Köln, café"
	for _, tag := range []string{"unknown-8bit", "x-unknown", "empty", "default", "utf-7", "cp-850", "iso-8859-1"} {
		for _, pos := range []string{"first", "after-body"} {
			name := "f24-" + tag + "-" + pos + ".eml"
			m := safeParseMessage(corpus(t, name))
			if m.FellBack || m.Subject == "" {
				t.Errorf("%s: no message (fell back %t, subject %q)", name, m.FellBack, m.Subject)
				continue
			}
			if a, ok := attNamed(m, "r.pdf"); !ok || !bytes.Equal(bytesOf(t, a), pdf) {
				t.Errorf("%s: r.pdf is not archived (attachments %v)", name, attNames(m))
			}
			if len(m.ParseGaps) != 0 {
				t.Errorf("%s: gaps %v, want none", name, m.ParseGaps)
			}
			for _, a := range m.Attachments {
				if a.GapKind != "" {
					t.Errorf("%s: attachment %q carries the gap %s", name, a.Filename, a.GapKind)
				}
			}
			if !utf8.ValidString(m.PlainBody) || strings.ContainsRune(m.PlainBody, utf8.RuneError) {
				t.Errorf("%s: the body is not clean UTF-8: %q", name, m.PlainBody)
			}
			if pos == "first" {
				if !strings.Contains(m.PlainBody, want) {
					t.Errorf("%s: body %q, want %q", name, m.PlainBody, want)
				}
			} else if !strings.Contains(m.PlainBody, "The body, in UTF-8.") {
				t.Errorf("%s: the first part is not the body: %q", name, m.PlainBody)
			}
			unknown := tag != "iso-8859-1"
			if tag == "empty" {
				if pos == "first" && !hasNote(m, noteLegacy8bit) {
					t.Errorf("%s: no legacy-8bit note (notes %v)", name, noteCodes(m))
				}
			} else if pos == "first" && unknown != hasNote(m, noteCharsetFallback) {
				t.Errorf("%s: charset-fallback note = %t, want %t (notes %v)", name, hasNote(m, noteCharsetFallback), unknown, noteCodes(m))
			}
			if unknown && !hasNote(m, noteContinuedAfterError) {
				t.Errorf("%s: no continued-after-error note: v1 stopped at this part (notes %v)", name, noteCodes(m))
			}
			if !unknown && len(m.ParseNotes) != 0 {
				t.Errorf("%s: the twin carries notes %v", name, noteCodes(m))
			}
		}
	}

	// A top-level unknown-8bit message with a quoted-printable body: no fallbackParse.
	m := safeParseMessage(corpus(t, "f24-unknown-8bit-single.eml"))
	if m.Subject != "Grüße café" {
		t.Errorf("single part: Subject %q, want the decoded %q (v1 kept the raw encoded word)", m.Subject, "Grüße café")
	}
	if !strings.Contains(m.PlainBody, want) {
		t.Errorf("single part: body %q, want %q (v1 kept it still quoted-printable)", m.PlainBody, want)
	}
	if hasNote(m, noteHeaderFallback) || len(m.ParseGaps) != 0 || m.FellBack {
		t.Errorf("single part: read by the header fallback or with a gap: notes %v gaps %v", noteCodes(m), m.ParseGaps)
	}
	if !hasNote(m, noteCharsetFallback) || !hasNote(m, noteEncodedWordFallback) {
		t.Errorf("single part: notes %v lack charset-fallback and encoded-word-fallback", noteCodes(m))
	}

	// What v1 did with the same input: the positive twin of the whole test.
	v1 := corpusV1Parse(corpus(t, "f24-unknown-8bit-first.eml"))
	if _, ok := attNamed(v1, "r.pdf"); ok {
		t.Error("v1 kept r.pdf after an unknown charset: the test no longer shows a defect")
	}
}
