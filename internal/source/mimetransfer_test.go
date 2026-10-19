package source

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// rawPart is the raw body of the part of a corpus item whose header block holds marker:
// the bytes after the part's blank line up to the next boundary line (the CRLF before the
// boundary belongs to the boundary).
func rawPart(t *testing.T, data []byte, marker string) []byte {
	t.Helper()
	i := bytes.Index(data, []byte(marker))
	if i < 0 {
		t.Fatalf("no part with %q", marker)
	}
	rest := data[i:]
	sep := "\r\n\r\n"
	if !bytes.Contains(rest, []byte(sep)) {
		sep = "\n\n"
	}
	start := bytes.Index(rest, []byte(sep)) + len(sep)
	rest = rest[start:]
	end := bytes.Index(rest, []byte("\r\n--"))
	if end < 0 {
		end = bytes.Index(rest, []byte("\n--"))
	}
	return rest[:end]
}

// covers: MA-334, R1, R10, R26
// F24 transfer encodings: 8-bit and quoted-printable (encoded) decode with lenient-cte;
// x-uuencode and x-foo keep their still-encoded bytes as <name>.undecoded and record a
// terminal undecoded-part gap, with the later parts archived and no stub; a base64 part
// cut short is archived as <name>.partial with a corrupt-part gap (fillable on an
// on-demand source, MC20), never under its real name. Twin: 7bit, base64 and
// quoted-printable. The re-run and fill half of the row, on the real pipeline, is
// TestUndecodedPartsAreTerminalAndTruncationIsFillableOnAnOnDemandSource.
func TestUnknownTransferEncodingsAreKeptAndRecorded(t *testing.T) {
	useFidelity(t)
	pdf := b64(t, corpusPDFBase64)
	requirePDF := func(name string, m *model.Message) {
		t.Helper()
		if a, ok := attNamed(m, "r.pdf"); !ok || !bytes.Equal(bytesOf(t, a), pdf) {
			t.Errorf("%s: r.pdf, a later part, is not archived (attachments %v)", name, attNames(m))
		}
	}
	noGapKind := func(name string, m *model.Message) {
		t.Helper()
		if len(m.ParseGaps) != 0 {
			t.Errorf("%s: gaps %v, want none", name, m.ParseGaps)
		}
		for _, a := range m.Attachments {
			if a.GapKind != "" {
				t.Errorf("%s: attachment %q carries the gap %s", name, a.Filename, a.GapKind)
			}
		}
	}

	t.Run("8-bit and quoted-printable (encoded) are read leniently", func(t *testing.T) {
		for _, c := range []struct {
			file string
			want []byte
		}{
			{"f24-cte-8-bit.eml", rawPart(t, corpus(t, "f24-cte-8-bit.eml"), `filename="note.txt"`)},
			{"f24-cte-qp-encoded.eml", []byte("Gr\xfc\xdfe caf\xe9\r\n")},
		} {
			m := safeParseMessage(corpus(t, c.file))
			a, ok := attNamed(m, "note.txt")
			if !ok || !bytes.Equal(bytesOf(t, a), c.want) {
				t.Errorf("%s: note.txt = %q (present %t), want %q", c.file, bytesOf2(a), ok, c.want)
			}
			if !hasNote(m, noteLenientCTE) {
				t.Errorf("%s: no lenient-cte note (notes %v)", c.file, noteCodes(m))
			}
			noGapKind(c.file, m)
			requirePDF(c.file, m)
			// v1 lost this part and every one after it.
			if v1 := corpusV1Parse(corpus(t, c.file)); len(v1.Attachments) != 0 {
				t.Errorf("%s: v1 kept %d attachments; the test no longer shows a defect", c.file, len(v1.Attachments))
			}
		}
	})

	t.Run("unknown encodings are kept as received, with a terminal gap", func(t *testing.T) {
		for _, c := range []struct{ file, cte string }{
			{"f24-cte-x-uuencode.eml", "x-uuencode"},
			{"f24-cte-x-foo.eml", "x-foo"},
		} {
			data := corpus(t, c.file)
			m := safeParseMessage(data)
			if m.Subject == stubSubject || m.FellBack {
				t.Fatalf("%s: a stub or a fallback: %q", c.file, m.Subject)
			}
			a, ok := attNamed(m, "note.txt.undecoded")
			if !ok {
				t.Fatalf("%s: no note.txt.undecoded (attachments %v)", c.file, attNames(m))
			}
			if want := rawPart(t, data, `filename="note.txt"`); !bytes.Equal(bytesOf(t, a), want) {
				t.Errorf("%s: the kept bytes are %q, want the part as received %q", c.file, bytesOf(t, a), want)
			}
			if a.GapKind != state.GapKindUndecodedPart || a.GapDetail == "" {
				t.Errorf("%s: gap %q %q, want an undecoded-part gap with a detail", c.file, a.GapKind, a.GapDetail)
			}
			if label := state.ParseGapLabel(a.GapKind, a.GapDetail); state.RecordGapClass(label, false) != state.ClassTerminal {
				t.Errorf("%s: undecoded-part is class %q on an on-demand source, want terminal on every source", c.file, state.RecordGapClass(label, false))
			}
			if _, real := attNamed(m, "note.txt"); real {
				t.Errorf("%s: the still-encoded bytes are under the real name", c.file)
			}
			requirePDF(c.file, m)
			if !bytes.Contains([]byte(m.PlainBody), []byte("Body before the odd part.")) {
				t.Errorf("%s: the body is %q", c.file, m.PlainBody)
			}
			if !hasNote(m, noteContinuedAfterError) || !hasNote(m, state.GapKindUndecodedPart) {
				t.Errorf("%s: notes %v lack continued-after-error or undecoded-part", c.file, noteCodes(m))
			}
		}
	})

	t.Run("a truncated base64 part is kept as .partial", func(t *testing.T) {
		m := safeParseMessage(corpus(t, "f24-cte-base64-truncated.eml"))
		// The 76 characters of the first line decode to 57 bytes; the 13 of the second, 12 of
		// them whole groups, to 9 more; the lone last character is the truncation.
		want := b64(t, "JVBERi0xLjQKJeLjz9MgbWltZS1jb3JwdXMgci5wZGYKMSAwIG9iajw8L1R5cGUvQ2F0YWxvZz4+ZW5kb2JqCnRy")
		a, ok := attNamed(m, "cut.pdf.partial")
		if !ok || !bytes.Equal(bytesOf(t, a), want) {
			t.Fatalf("attachments %v; want cut.pdf.partial holding the %d decoded bytes", attNames(m), len(want))
		}
		if _, real := attNamed(m, "cut.pdf"); real {
			t.Error("the truncated bytes are under the real name")
		}
		if a.GapKind != state.GapKindCorruptPart {
			t.Errorf("gap %q, want corrupt-part", a.GapKind)
		}
		// Fillable on an on-demand source, terminal on a source that delivers a message whole.
		label := state.ParseGapLabel(a.GapKind, a.GapDetail)
		if state.RecordGapClass(label, false) != state.ClassFillable || state.RecordGapClass(label, true) != state.ClassTerminal {
			t.Errorf("corrupt-part is %q in Missing and %q in Terminal, want fillable and terminal", state.RecordGapClass(label, false), state.RecordGapClass(label, true))
		}
		// v1 kept the same prefix under the real name, which is what the name must never do.
		if v1 := corpusV1Parse(corpus(t, "f24-cte-base64-truncated.eml")); len(v1.Attachments) != 1 || v1.Attachments[0].Filename != "cut.pdf" {
			t.Errorf("v1: %v; the test no longer shows the defect it fixes", v1.Attachments)
		}
	})

	t.Run("7bit, base64 and quoted-printable are untouched", func(t *testing.T) {
		for _, file := range []string{"f24-cte-7bit.eml", "f24-cte-base64.eml", "f24-cte-quoted-printable.eml"} {
			m := safeParseMessage(corpus(t, file))
			noGapKind(file, m)
			requirePDF(file, m)
			for _, n := range m.ParseNotes {
				if n.Code != noteTextAttachmentBytes {
					t.Errorf("%s: unexpected note %s", file, n.Code)
				}
			}
		}
	})
}

// bytesOf2 is bytesOf without the test, for a message: an absent attachment reads as nil.
func bytesOf2(a model.Attachment) []byte {
	if a.WriteTo == nil {
		return nil
	}
	var buf bytes.Buffer
	_, _ = a.WriteTo(&buf)
	return buf.Bytes()
}

// covers: MA-337, R1, R5, R26
// C1-1: a text attachment's bytes are its transfer-decoded source bytes, never
// charset-converted. Latin-1 text/csv, ISO-8859-1 XML, UTF-16 and Shift_JIS text, a
// cp1252 calendar and a text/html attachment each equal the bytes the sender's transfer
// encoding held (v1 re-encoded every one but the UTF-8 twin); an 8bit, LF-only text
// attachment read from an mbox has the bytes as go-mbox frames them, which are CRLF-ended
// (MC38: the store's bytes, which can differ from the sender's file); a truncated base64
// attachment is .partial, never under its real name. The zip digest and `verify` half of
// the row, on the real pipeline, is TestTextAttachmentsReachTheZipAsTheirSourceBytes.
func TestTextAttachmentsKeepTheirSourceBytes(t *testing.T) {
	useFidelity(t)
	data := corpus(t, "c11-text-attachments.eml")
	m := safeParseMessage(data)
	v1 := corpusV1Parse(data)
	want := map[string][]byte{
		"latin1.csv": b64(t, "bmFtZTtjaXR5DQpN/GxsZXI7S/Zsbg0KSm9z6Tta/HJpY2gNCg=="),
		"latin1.xml": qp(t, "<?xml version=3D\"1.0\" encoding=3D\"ISO-8859-1\"?>\r\n<invoice><name>Gr=FC=DFe K=F6ln</name></invoice>\r\n"),
		"utf16.txt":  b64(t, "//5HAHIA/ADfAGUAIADlZSxnnooNAAoA"),
		"sjis.txt":   b64(t, "k/qWe4zqgsyDgYOCDQo="),
		"cp1252.ics": b64(t, "QkVHSU46VkNBTEVOREFSDQpCRUdJTjpWRVZFTlQNClNVTU1BUlk6Q2Fm6SCTcXVvdGVklA0KRU5EOlZFVkVOVA0KRU5EOlZDQUxFTkRBUg0K"),
		"page.html":  b64(t, "PGh0bWw+PGJvZHk+PHA+R3L832UgYXVzIEv2bG48L3A+PC9ib2R5PjwvaHRtbD4NCg=="),
	}
	for name, w := range want {
		a, ok := attNamed(m, name)
		if !ok {
			t.Errorf("%s is not archived (attachments %v)", name, attNames(m))
			continue
		}
		if got := bytesOf(t, a); !bytes.Equal(got, w) {
			t.Errorf("%s: archived %q, want the source bytes %q", name, got, w)
		}
		// The positive twin: v1 stored different bytes, so a digest over them vouched for
		// bytes the store never held.
		if va, ok := attNamed(v1, name); !ok || bytes.Equal(bytesOf(t, va), w) {
			t.Errorf("%s: v1 stored the source bytes (present %t): the test no longer shows the defect", name, ok)
		}
	}
	if !hasNote(m, noteTextAttachmentBytes) {
		t.Errorf("no text-attachment-bytes note (notes %v)", noteCodes(m))
	}

	// utf-8 twin: nothing converted, nothing to say.
	u := safeParseMessage(corpus(t, "c11-utf8-attachment.eml"))
	if hasNote(u, noteTextAttachmentBytes) || len(u.ParseGaps) != 0 {
		t.Errorf("the utf-8 twin carries notes %v gaps %v", noteCodes(u), u.ParseGaps)
	}

	// 8bit, LF-only, through go-mbox's framing.
	lf := corpus(t, "c11-8bit-attachment-lf.eml")
	direct := safeParseMessage(lf)
	if a, ok := attNamed(direct, "lf.txt"); !ok || !bytes.Equal(bytesOf(t, a), rawPart(t, lf, `filename="lf.txt"`)) {
		t.Errorf("lf.txt parsed directly is %q, want the part as sent", bytesOf2(a))
	}
	path := filepath.Join(t.TempDir(), "box")
	if err := os.WriteFile(path, append([]byte("From - Mon Jan 01 00:00:00 2024\n"), lf...), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	var got *model.Message
	if err := src.Walk(func(_ []string, m *model.Message) error { got = m; return nil }); err != nil || got == nil {
		t.Fatalf("walk: %v", err)
	}
	a, ok := attNamed(got, "lf.txt")
	if !ok {
		t.Fatalf("lf.txt is not archived from the mbox (attachments %v)", attNames(got))
	}
	if want := []byte("Gr\xfc\xdfe\r\nK\xf6ln\r\n"); !bytes.Equal(bytesOf(t, a), want) {
		t.Errorf("lf.txt from an mbox is %q, want the bytes as go-mbox frames them, %q", bytesOf(t, a), want)
	}

	// A truncated base64 attachment is .partial under its decoded prefix.
	tm := safeParseMessage(corpus(t, "f24-cte-base64-truncated.eml"))
	if _, ok := attNamed(tm, "cut.pdf.partial"); !ok {
		t.Errorf("a truncated attachment is not .partial (attachments %v)", attNames(tm))
	}
}
