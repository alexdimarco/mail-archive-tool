package source

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/model"
)

// injectPanics makes the frozen v1 parse, the content parse, or both panic until the
// test ends or the returned function is called. No input is known that panics v1 (see
// TestMimeCorpusIsComplete), so the panic branches of safeParseMessage are reached
// through these seams.
func injectPanics(t *testing.T, v1, content bool) (restore func()) {
	t.Helper()
	oldV1, oldContent := v1Parse, parseContent
	restore = func() { v1Parse, parseContent = oldV1, oldContent }
	t.Cleanup(restore)
	if v1 {
		v1Parse = func([]byte) *model.Message { panic("injected panic in the v1 parse") }
	}
	if content {
		parseContent = func([]byte) *model.Message { panic("injected panic in the content parse") }
	}
	return restore
}

// changedContent stands in for a fidelity parser: the v1 content with every field an
// identity input is built from moved (the subject, sender, recipients, Message-ID,
// date, bodies, attachment names and count).
func changedContent(data []byte) *model.Message {
	m := parseMessage(data)
	m.Subject = "decoded: " + m.Subject
	m.SenderEmail = "changed-" + m.SenderEmail
	m.To, m.Cc, m.Bcc = m.To+" changed", m.Cc+" changed", m.Bcc+" changed"
	m.InternetMessageID += "-changed"
	if !m.IdentityDate.IsZero() {
		// A message with no Date header keeps none: the design's non-goal is that Date
		// extraction does not change, and a Date-less identity reads the message's own
		// Received (which Graph overrides, MA-233), so a parser that invented one would
		// move it. A dated message may have its dates moved: the terms hold the Date.
		m.IdentityDate = m.IdentityDate.Add(time.Hour)
		m.Received = m.Received.Add(time.Hour)
	}
	m.HTMLBody += "<p>split body</p>"
	m.PlainBody += "\n\nsplit body"
	m.RTFBody += "rtf"
	for i := range m.Attachments {
		m.Attachments[i].Filename = "renamed-" + m.Attachments[i].Filename
	}
	m.Attachments = append(m.Attachments, model.Attachment{Filename: "inline.png", Size: 7})
	return m
}

// corpusPortion is a golden's text up to the stub item: the corpus's own blocks.
func corpusPortion(t *testing.T, golden string) string {
	t.Helper()
	i := strings.Index(golden, "== "+mimeV1StubName)
	if i < 0 {
		t.Fatal("the golden has no stub item")
	}
	return golden[:i]
}

// covers: MA-330, R3, R2, R27, S49
// Identity, Fingerprint, ContentDigest and the local and live keys of every corpus
// message equal the identity golden through the production routing, for messages the
// content parser changes (every identity input moved) as well as unchanged ones, and
// for a message v1 panics on, whose identity comes from stubIdentityTerms; the PST twin
// is the golden's support.pst items.
func TestFidelityParserKeepsTheIdentityGoldenOnTheCorpus(t *testing.T) {
	items := loadMimeCorpus(t)
	wantID, _, err := goldenBody(mimeGoldenIdentity)
	if err != nil {
		t.Fatalf("%s: %v", mimeGoldenIdentity, err)
	}
	wantContent, _, err := goldenBody(mimeGoldenContent)
	if err != nil {
		t.Fatalf("%s: %v", mimeGoldenContent, err)
	}

	// The stub safeParseMessage leaves when the v1 parse panics: today's constant stub,
	// with the identity stubIdentityTerms gives it.
	restore := injectPanics(t, true, false)
	stub := safeParseMessage([]byte("any bytes"))
	restore()
	if stub == nil || stub.IdentityTerms == nil {
		t.Fatalf("the stub carries no identity terms: %+v", stub)
	}

	t.Run("content from v1", func(t *testing.T) {
		id, content := renderGoldens(t, items, safeParseMessage, stub)
		if id != wantID {
			t.Errorf("the identity golden does not hold through safeParseMessage at %s", firstDiff(wantID, id))
		}
		// S1 changes nothing a user can see: the content is v1's, byte for byte.
		if corpusPortion(t, content) != corpusPortion(t, wantContent) {
			t.Errorf("the content moved at %s", firstDiff(corpusPortion(t, wantContent), corpusPortion(t, content)))
		}
	})

	t.Run("content a fidelity parser changed", func(t *testing.T) {
		old := parseContent
		parseContent = changedContent
		t.Cleanup(func() { parseContent = old })
		id, content := renderGoldens(t, items, safeParseMessage, stub)
		if id != wantID {
			t.Errorf("a content parser that changes every identity input moved the identity at %s", firstDiff(wantID, id))
		}
		// The positive twin: the content did change, and without the terms so would the
		// identity.
		if corpusPortion(t, content) == corpusPortion(t, wantContent) {
			t.Error("the stand-in content parser changed nothing, so this case proves nothing")
		}
		var unmoved []string
		for _, it := range items {
			m := safeParseMessage(it.data)
			bare := *m
			bare.IdentityTerms = nil
			if bare.Fingerprint() == m.Fingerprint() {
				unmoved = append(unmoved, it.name)
			}
		}
		if len(unmoved) > 0 {
			t.Errorf("the changed content has the same fingerprint as its terms for %d items (first %s): the terms are the content", len(unmoved), unmoved[0])
		}
	})

	t.Run("v1 panics: the stub and its identity", func(t *testing.T) {
		injectPanics(t, true, false)
		for _, it := range items {
			m := safeParseMessage(it.data)
			if m == nil || m.IdentityTerms == nil {
				t.Fatalf("%s: no message or no terms", it.name)
			}
			if got, want := m.Identity(), model.LegacyStubIdentity(); got != want {
				t.Errorf("%s: Identity() = %q, want the constant stub's %q", it.name, got, want)
			}
			if m.Subject != stubSubject || m.Raw != nil || m.TransportHeaders != "" || m.V1Inventory != nil || len(m.Attachments) != 0 {
				t.Errorf("%s: the message is not today's stub: %+v", it.name, m)
			}
		}
	})

	t.Run("the content parser panics: v1's content", func(t *testing.T) {
		injectPanics(t, false, true)
		id, content := renderGoldens(t, items, safeParseMessage, stub)
		if id != wantID {
			t.Errorf("a panicking content parser moved the identity at %s", firstDiff(wantID, id))
		}
		if corpusPortion(t, content) != corpusPortion(t, wantContent) {
			t.Errorf("a panicking content parser did not fall back to v1's content, at %s", firstDiff(corpusPortion(t, wantContent), corpusPortion(t, content)))
		}
		for _, it := range items {
			if m := safeParseMessage(it.data); !m.FellBack {
				t.Errorf("%s: the fallback is not marked", it.name)
			}
		}
	})

	t.Run("every message carries its terms and inventory", func(t *testing.T) {
		for _, it := range items {
			m := safeParseMessage(it.data)
			if m.IdentityTerms == nil {
				t.Fatalf("%s: no identity terms", it.name)
			}
			if m.FellBack {
				t.Errorf("%s: marked as a fallback with nothing wrong", it.name)
			}
			v1, _ := replayV1(it.data)
			if len(m.V1Inventory) != len(v1.Attachments) {
				t.Fatalf("%s: inventory of %d items for %d v1 attachments", it.name, len(m.V1Inventory), len(v1.Attachments))
			}
			for i, a := range v1.Attachments {
				var buf bytes.Buffer
				if _, err := a.WriteTo(&buf); err != nil {
					t.Fatal(err)
				}
				got := m.V1Inventory[i]
				if got.Index != i || got.Label != a.Label(i) || got.Size != int64(buf.Len()) || got.SHA256 != sha256.Sum256(buf.Bytes()) {
					t.Errorf("%s: inventory item %d = %+v, want index %d label %q size %d sha %x", it.name, i, got, i, a.Label(i), buf.Len(), sha256.Sum256(buf.Bytes()))
				}
			}
		}
	})
}

// covers: MA-330, R3, R27
// stubIdentityTerms reproduces today's constant stub, whatever the bytes: two different
// messages v1 panics on still share one identity until the owner rules on the fold
// (rulings J5, J8; ESC-1).
func TestStubIdentityTermsIsTheConstantStub(t *testing.T) {
	a, b := stubIdentityTerms([]byte("one")), stubIdentityTerms([]byte("another, longer message"))
	ma := &model.Message{IdentityTerms: &a}
	mb := &model.Message{IdentityTerms: &b}
	want := model.LegacyStubIdentity()
	if ma.Identity() != want || mb.Identity() != want {
		t.Errorf("stub identities %q and %q, want %q for both", ma.Identity(), mb.Identity(), want)
	}
	if a.Subject != "(unreadable message)" || a.InternetMessageID != "" || a.PlainBody != "" || len(a.Attachments) != 0 {
		t.Errorf("stubIdentityTerms = %+v, want only today's subject", a)
	}
}
