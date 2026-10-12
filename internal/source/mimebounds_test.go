package source

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// hostile builds a message with the given Content-Type and body (CRLF line ends).
func hostile(contentType, body string) []byte {
	return []byte("From: Mallory <m@x.example>\r\nTo: b@x.example\r\nSubject: hostile\r\n" +
		"Message-ID: <hostile@x.example>\r\nDate: Mon, 02 Jan 2023 15:04:05 +0000\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: " + contentType + "\r\n\r\n" + body)
}

// stopsV1 is a first part that stops the v1 parser (its charset is not in v1's table), so
// v1 holds nothing of what follows and the walker's own content stands: the path on which
// a bound's gap is recorded. Without it v1 reads past the bound, the walker holds less
// than v1, and the message falls back to v1's content with a visible gap (MC4): the
// other outcome the row allows.
const stopsV1 = "--bd\r\nContent-Type: text/plain; charset=\"unknown-8bit\"\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nfirst=E9\r\n"

// within runs f and fails the test if it takes longer than d: a hostile shape must
// complete, whatever it holds (R10).
func within(t *testing.T, d time.Duration, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("did not finish within %v", d)
	}
}

// healthy is the next message in an mbox: it must parse as ever after a hostile one.
func healthy(t *testing.T) {
	t.Helper()
	data := corpus(t, "ctl-plain-ascii.eml")
	m := safeParseMessage(data)
	want := corpusV1Parse(data)
	if m.Subject != want.Subject || m.PlainBody != want.PlainBody || m.FellBack || len(m.ParseGaps) != 0 || len(m.ParseNotes) != 0 {
		t.Errorf("the message after a hostile one is %q %q (fell back %t, gaps %v, notes %v)", m.Subject, m.PlainBody, m.FellBack, m.ParseGaps, noteCodes(m))
	}
}

// requireNoStub says the message is the walker's reading (or v1's content), never the
// "(unreadable message)" stub: a stub is only where v1 itself produced one.
func requireNoStub(t *testing.T, name string, m *model.Message) {
	t.Helper()
	if m == nil || m.Subject == stubSubject {
		t.Errorf("%s: a stub", name)
	}
}

// v1ReadsNothing makes the frozen replay hold no content, as if it had stopped before
// the hostile part, until the returned function (or the end of the test) restores it.
func v1ReadsNothing(t *testing.T) (restore func()) {
	t.Helper()
	old := v1Parse
	restore = func() { v1Parse = old }
	v1Parse = func(data []byte) *model.Message { return &model.Message{Raw: data} }
	t.Cleanup(restore)
	return restore
}

// bothOutcomes parses a hostile message twice and checks each outcome the row allows:
// as production routes it, the walker's content with the bound's gap, or, when v1 read
// past the bound, v1's content with a visible fidelity-fallback gap; and with v1 reading
// nothing, always the walker's content with the gap (check).
func bothOutcomes(t *testing.T, name string, data []byte, check func(m *model.Message)) {
	t.Helper()
	var prod, walker *model.Message
	within(t, 60*time.Second, func() {
		prod = safeParseMessage(data)
		old := v1Parse
		v1Parse = func(d []byte) *model.Message { return &model.Message{Raw: d} }
		walker = safeParseMessage(data)
		v1Parse = old
	})
	requireNoStub(t, name, prod)
	requireNoStub(t, name, walker)
	if prod.FellBack {
		want := corpusV1Parse(data)
		if len(prod.ParseGaps) != 1 || prod.ParseGaps[0].Kind != state.GapKindFidelityFallback || !strings.Contains(prod.ParseGaps[0].Detail, "held less") {
			t.Errorf("%s: fell back with gaps %v, want one visible fidelity-fallback gap", name, prod.ParseGaps)
		}
		if prod.PlainBody != want.PlainBody || len(prod.Attachments) != len(want.Attachments) {
			t.Errorf("%s: the fallback is not v1's content (%d attachments, v1's %d)", name, len(prod.Attachments), len(want.Attachments))
		}
	} else {
		check(prod)
	}
	if walker.FellBack {
		t.Errorf("%s: fell back although v1 held nothing", name)
	}
	check(walker)
}

// deepMultipart is a message whose multiparts nest levels deep, the innermost holding
// one text part "deep body"; with first, a part that stops v1 comes before the chain.
func deepMultipart(levels int, first bool) []byte {
	body := fmt.Sprintf("--b%d\r\nContent-Type: text/plain\r\n\r\ndeep body\r\n--b%d--\r\n", levels, levels)
	for i := levels - 1; i >= 1; i-- {
		body = fmt.Sprintf("--b%d\r\nContent-Type: multipart/mixed; boundary=\"b%d\"\r\n\r\n%s--b%d--\r\n", i, i+1, body, i)
	}
	if first {
		body = strings.ReplaceAll(stopsV1, "--bd", "--b1") + body
	}
	return hostile(`multipart/mixed; boundary="b1"`, body)
}

// flatMultipart is a message of n attachments in one multipart, after a part that stops
// v1 when first is set.
func flatMultipart(n int, first bool) []byte {
	var b strings.Builder
	if first {
		b.WriteString(stopsV1)
	}
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "--bd\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=\"f%d.bin\"\r\n\r\nx\r\n", i)
	}
	b.WriteString("--bd--\r\n")
	return hostile(`multipart/mixed; boundary="bd"`, b.String())
}

// covers: MA-349, R10, R1, R19, R26
// Bounds and isolation, the parser half of the row. Each hostile shape completes within a
// generous deadline, records a structure-limit or corrupt-part gap (one part only) or
// reverts the message to v1's content with a visible fidelity-fallback gap, and never
// yields a stub; the next message parses as ever. The shapes: multipart depth 33, 10,001
// leaves, 64+ lenient parameters, filename*4294967295*= (bounded, no allocation by
// index), an oversized part header and an oversized top-level header (MC22), an
// encoded-word bomb, a panic in one part's decoder, a panic in the fidelity parser and a
// failed "fidelity contains v1" check (MC4). The export side of the row is
// TestHostileMIMEExportSideIsIsolated (MF-S4, MF-S7); more than 32 gaps in a record is
// TestManyParseGapsAreBoundedInTheRecord.
func TestHostileMIMEIsBoundedAndNeverYieldsAStub(t *testing.T) {
	useFidelity(t)
	const deadline = 60 * time.Second

	t.Run("multipart depth 33", func(t *testing.T) {
		bothOutcomes(t, "depth 33", deepMultipart(33, true), func(m *model.Message) {
			t.Helper()
			a, ok := attNamed(m, "remaining-parts.undecoded")
			if !ok || a.GapKind != state.GapKindStructureLimit || !bytes.Contains(bytesOf(t, a), []byte("deep body")) {
				t.Errorf("depth 33: attachments %v, want remaining-parts.undecoded (structure-limit) holding the unread part", attNames(m))
			}
			if strings.Contains(m.PlainBody, "deep body") {
				t.Errorf("depth 33: the part past the bound was read: %q", m.PlainBody)
			}
		})
		bothOutcomes(t, "depth 33, v1 reads it", deepMultipart(33, false), func(m *model.Message) {})
		// Twin: 32 levels are within the bound and are read, with no gap.
		var twin *model.Message
		within(t, deadline, func() { twin = safeParseMessage(deepMultipart(32, false)) })
		if !strings.Contains(twin.PlainBody, "deep body") || len(twin.Attachments) != 0 || len(twin.ParseGaps) != 0 || twin.FellBack {
			t.Errorf("depth 32: body %q attachments %v gaps %v", twin.PlainBody, attNames(twin), twin.ParseGaps)
		}
		healthy(t)
	})

	t.Run("10,001 leaves", func(t *testing.T) {
		// The part that stops v1 is the first of the 10,001; the walker reads 10,000 parts,
		// the last attachment is past the bound.
		bothOutcomes(t, "10,001 leaves", flatMultipart(10000, true), func(m *model.Message) {
			t.Helper()
			a, ok := attNamed(m, "remaining-parts.undecoded")
			if !ok || a.GapKind != state.GapKindStructureLimit {
				t.Fatalf("10,001 leaves: no remaining-parts.undecoded (%d attachments)", len(m.Attachments))
			}
			if len(m.Attachments) != maxParts {
				t.Errorf("10,001 leaves: %d attachments, want %d: %d-1 read and one blob", len(m.Attachments), maxParts, maxParts)
			}
			rest := bytesOf(t, a)
			if !bytes.HasPrefix(rest, []byte("--bd")) || !bytes.Contains(rest, []byte(`filename="f10000.bin"`)) || bytes.Contains(rest, []byte(`filename="f9999.bin"`)) {
				t.Errorf("10,001 leaves: the blob does not start at the first unread part: %.80q", rest)
			}
		})
		bothOutcomes(t, "10,001 leaves, v1 reads it", flatMultipart(10001, false), func(m *model.Message) {})
		// Twin: exactly 10,000 leaves are within the bound.
		var twin *model.Message
		within(t, deadline, func() { twin = safeParseMessage(flatMultipart(10000, false)) })
		if len(twin.Attachments) != maxParts || len(twin.ParseGaps) != 0 || twin.FellBack {
			t.Errorf("10,000 leaves: %d attachments, gaps %v, fell back %t; want all read and no gap", len(twin.Attachments), twin.ParseGaps, twin.FellBack)
		}
		healthy(t)
	})

	t.Run("64 and more lenient parameters", func(t *testing.T) {
		var b strings.Builder
		b.WriteString(`multipart/mixed; boundary="bd"`)
		for i := 0; i < 500; i++ {
			fmt.Fprintf(&b, "; p%d=v%d", i, i)
		}
		b.WriteString("; q=a b") // an unquoted space: the strict parser rejects the header
		var m *model.Message
		within(t, deadline, func() {
			m = safeParseMessage(hostile(b.String(), "--bd\r\nContent-Type: text/plain\r\n\r\nstill here\r\n--bd--\r\n"))
		})
		requireNoStub(t, "many parameters", m)
		if !strings.Contains(m.PlainBody, "still here") || !hasNote(m, noteLenientParams) {
			t.Errorf("many parameters: body %q notes %v", m.PlainBody, noteCodes(m))
		}
		healthy(t)
	})

	t.Run("a continuation index of 4294967295", func(t *testing.T) {
		var m *model.Message
		within(t, deadline, func() {
			m = safeParseMessage(hostile(`multipart/mixed; boundary="bd"`,
				"--bd\r\nContent-Type: application/octet-stream\r\n"+
					"Content-Disposition: attachment; filename*4294967295*=utf-8''x; filename*0*=utf-8''y; filename=a b.txt\r\n\r\nxyz\r\n--bd--\r\n"))
		})
		requireNoStub(t, "a huge continuation index", m)
		if a, ok := attNamed(m, "a b.txt"); !ok || string(bytesOf(t, a)) != "xyz" {
			t.Errorf("a huge continuation index: attachments %v", attNames(m))
		}
		healthy(t)
	})

	t.Run("an oversized part header", func(t *testing.T) {
		padded := "--bd\r\nX-Pad: " + strings.Repeat("a", 2<<20) + "\r\nContent-Type: text/plain\r\n\r\nthe padded part\r\n"
		sibling := "--bd\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"sibling.pdf\"\r\n\r\nthe sibling\r\n--bd--\r\n"
		bothOutcomes(t, "an oversized part header",
			hostile(`multipart/mixed; boundary="bd"`, stopsV1+padded+sibling), func(m *model.Message) {
				t.Helper()
				a, ok := attNamed(m, "part-2.undecoded")
				if !ok || a.GapKind != state.GapKindStructureLimit || !bytes.Contains(bytesOf(t, a), []byte("the padded part")) {
					t.Errorf("an oversized part header: attachments %v, want part-2.undecoded (structure-limit) holding the part", attNames(m))
				}
				if s, ok := attNamed(m, "sibling.pdf"); !ok || string(bytesOf(t, s)) != "the sibling" {
					t.Errorf("an oversized part header cost its sibling: attachments %v", attNames(m))
				}
			})
		healthy(t)
	})

	t.Run("an oversized top-level header", func(t *testing.T) {
		var m *model.Message
		within(t, deadline, func() {
			m = safeParseMessage([]byte("Subject: big\r\nX-Pad: " + strings.Repeat("a", 2<<20) + "\r\n\r\nthe body\r\n"))
		})
		requireNoStub(t, "an oversized header", m)
		if !hasNote(m, noteHeaderFallback) || !strings.Contains(m.PlainBody, "the body") {
			t.Errorf("an oversized header: notes %v body %q", noteCodes(m), m.PlainBody)
		}
		healthy(t)
	})

	t.Run("an encoded-word bomb", func(t *testing.T) {
		var m *model.Message
		within(t, deadline, func() {
			m = safeParseMessage([]byte("Subject: " + strings.Repeat("=?x-foo?Q?=E9?= =?utf-8?B?QQ==?= ", 20000) + "\r\n\r\nbody\r\n"))
		})
		requireNoStub(t, "an encoded-word bomb", m)
		healthy(t)
	})

	t.Run("a panic in one part's decoder", func(t *testing.T) {
		restoreV1 := v1ReadsNothing(t) // v1 decoded the part the injected fault fails on; the walker's own content is what is asserted
		partDecodeHook = func(n int) {
			if n == 2 {
				panic("injected panic in a part decoder")
			}
		}
		defer func() { partDecodeHook = nil }()
		m := safeParseMessage(hostile(`multipart/mixed; boundary="bd"`,
			"--bd\r\nContent-Type: text/plain\r\n\r\nthe body\r\n"+
				"--bd\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"two.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nAAAA\r\n"+
				"--bd\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"three.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nAAAA\r\n--bd--\r\n"))
		requireNoStub(t, "a panic in a decoder", m)
		a, ok := attNamed(m, "part-2.undecoded")
		if !ok || a.GapKind != state.GapKindCorruptPart || string(bytesOf(t, a)) != "AAAA" {
			t.Errorf("a panic in a decoder: attachments %v, want part-2.undecoded (corrupt-part) holding the part as received", attNames(m))
		}
		if _, ok := attNamed(m, "three.pdf"); !ok || !strings.Contains(m.PlainBody, "the body") {
			t.Errorf("a panic in a decoder cost another part: attachments %v body %q", attNames(m), m.PlainBody)
		}
		partDecodeHook = nil
		restoreV1()
		healthy(t)
	})

	t.Run("a panic in the fidelity parser", func(t *testing.T) {
		defer injectPanics(t, false, true)()
		data := corpus(t, "f24-cte-x-uuencode.eml")
		m := safeParseMessage(data)
		v1 := corpusV1Parse(data)
		requireNoStub(t, "a panic in the parser", m)
		if !m.FellBack || m.Subject != v1.Subject || m.PlainBody != v1.PlainBody || len(m.Attachments) != len(v1.Attachments) {
			t.Errorf("a panic in the parser: not v1's content (fell back %t)", m.FellBack)
		}
		if len(m.ParseGaps) != 1 || m.ParseGaps[0].Kind != state.GapKindFidelityFallback || !strings.Contains(m.ParseGaps[0].Detail, "failed while reading") {
			t.Errorf("a panic in the parser: gaps %v, want one visible fidelity-fallback gap naming where", m.ParseGaps)
		}
		if m.Identity() != v1.Identity() || m.Fingerprint() != v1.Fingerprint() {
			t.Error("a panic in the parser moved the identity")
		}
	})

	t.Run("a capture that holds less than v1", func(t *testing.T) {
		old := parseContent
		defer func() { parseContent = old }()
		parseContent = func(data []byte) *model.Message {
			m := parseMessage(data)
			m.Attachments = nil // the fidelity capture lost the attachment v1 had
			return m
		}
		data := corpus(t, "ctl-attachment-base64.eml")
		m := safeParseMessage(data)
		v1 := corpusV1Parse(data)
		requireNoStub(t, "a capture that holds less", m)
		if len(v1.Attachments) == 0 || len(m.Attachments) != len(v1.Attachments) || !m.FellBack {
			t.Fatalf("a capture that holds less: %d attachments, v1's %d, fell back %t; want v1's content", len(m.Attachments), len(v1.Attachments), m.FellBack)
		}
		if len(m.ParseGaps) != 1 || m.ParseGaps[0].Kind != state.GapKindFidelityFallback || !strings.Contains(m.ParseGaps[0].Detail, "held less") || !hasNote(m, noteFidelityRegression) {
			t.Errorf("a capture that holds less: gaps %v notes %v, want a fidelity-fallback gap and a fidelity-regression note", m.ParseGaps, noteCodes(m))
		}
		// The positive twin: the same capture, whole, passes the check.
		parseContent = nil
		if ok := safeParseMessage(data); ok.FellBack || len(ok.ParseGaps) != 0 {
			t.Errorf("the check fails a capture that holds all of v1: gaps %v", ok.ParseGaps)
		}
	})
}

// covers: MA-349, R3, R26
// A stub results only where v1 itself produced one (MC6): when the frozen replay panics
// but the walker reads the message, its content is shown under the stub's identity terms
// with a visible terminal identity-stub gap; when both panic, it is today's stub.
//
// The stub's terms carry no date, so Identity() reads the message's own: the identity is
// the stub hash over that date, not the one constant of the old stub (F33's collapse).
// Closing that needs a zero-date marker in IdentityTerms or fault-isolation's stub
// (ESC-1); no input is known that panics v1, so no archive holds such a message.
func TestAMessageV1CannotReadIsShownWithTheStubIdentityAndAGap(t *testing.T) {
	useFidelity(t)
	data := corpus(t, "ctl-plain-ascii.eml")
	want := corpusV1Parse(data)

	restore := injectPanics(t, true, false)
	m := safeParseMessage(data)
	restore()
	if m.Subject != want.Subject || !strings.Contains(m.PlainBody, strings.TrimSpace(want.PlainBody)) {
		t.Errorf("the walker's content is not shown: %q %q", m.Subject, m.PlainBody)
	}
	if terms := stubIdentityTerms(data); m.IdentityTerms == nil || m.IdentityTerms.Subject != terms.Subject || m.IdentityTerms.InternetMessageID != terms.InternetMessageID ||
		m.IdentityTerms.PlainBody != terms.PlainBody || len(m.IdentityTerms.Attachments) != 0 {
		t.Errorf("the identity terms are %+v, want stubIdentityTerms", m.IdentityTerms)
	}
	if len(m.ParseGaps) != 1 || m.ParseGaps[0].Kind != state.GapKindIdentityStub || m.FellBack {
		t.Fatalf("gaps %v fell back %t, want one identity-stub gap", m.ParseGaps, m.FellBack)
	}
	if label := state.ParseGapLabel(m.ParseGaps[0].Kind, m.ParseGaps[0].Detail); state.RecordGapClass(label, false) != state.ClassTerminal {
		t.Errorf("identity-stub is %q on an on-demand source, want terminal on every source", state.RecordGapClass(label, false))
	}

	restore = injectPanics(t, true, true)
	stub := safeParseMessage(data)
	restore()
	if stub.Subject != stubSubject || stub.Identity() != model.LegacyStubIdentity() || len(stub.ParseGaps) != 0 {
		t.Errorf("v1 and the walker both panicking: %q gaps %v, want today's stub", stub.Subject, stub.ParseGaps)
	}
}
