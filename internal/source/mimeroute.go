package source

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	v1charset "mail-archive-tool/internal/source/parsev1/_dep/charset"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// The routing of design-mime-fidelity §4.2 once slice S2 is on: identity from the frozen
// v1 replay, content from the fidelity walker, and the "fidelity contains v1" check that
// keeps the walker never worse than v1 (P13, MC4, MC19).

// fidelityContent says safeParseMessage routes as §4.2 describes: the message the user
// sees comes from the fidelity walker, and v1's content is the fallback. Off, it routes
// as slice S1 did: v1's content, no check, no gaps.
//
// It is ON (owner-approved 2026-10-12, rulings §13.29), together with ParseSchemeCurrent
// 2. Turning it on narrowed four assertions of MF-S1 tests that asserted S1's behaviour:
//   - identityrouting_test.go, TestFidelityParserKeepsTheIdentityGoldenOnTheCorpus:
//     "content from v1" (the content now moves, the identity does not), "v1 panics: the
//     stub and its identity" (the walker's content is shown under the stub's identity
//     with an identity-stub gap) and "the content parser panics: v1's content" (the
//     fallback's body text is valid UTF-8 and carries a fidelity-fallback gap);
//   - export/mimeidentity_test.go, TestMixedVersionsKeepIdentityAndStems: a record whose
//     message carries a note newer than its scheme is not stamped on re-read (MS1-AI1).
//
// The identity goldens did not change. A message v1 panics on keeps the stub identity
// because its terms carry NoIdentityDate: the walker's message has display dates that
// identityTime would otherwise fold in.
var fidelityContent = true

// contentParser is the content parser of the mode safeParseMessage is in: a test's
// stand-in (parseContent) when one is set, else the walker or, with the switch off, v1.
func contentParser() func([]byte) *model.Message {
	switch {
	case parseContent != nil:
		return parseContent
	case fidelityContent:
		return parseFidelity
	}
	return parseMessage
}

// safeParseFidelity is safeParseMessage with the fidelity content (design §4.2, steps
// 1 to 5).
func safeParseFidelity(data []byte) *model.Message {
	v1, v1Panicked := replayV1(data)
	if v1Panicked || v1 == nil {
		// The replay panicked: the identity is the stub's, as before. If the walker reads
		// the message its content is shown, with a visible terminal identity-stub gap (MC6);
		// if it panics too, the message is the stub, as it was before the walker existed.
		terms := stubIdentityTerms(data)
		m, _, _ := fidelityContentOf(data)
		if m == nil {
			m = unreadableStub()
		} else {
			m.TransportHeaders = headerBlock(data)
			m.ParseGaps = append(m.ParseGaps, model.ParseGap{
				Kind:   state.GapKindIdentityStub,
				Detail: "an earlier parser could not read this message, so it is filed under the placeholder identity that parser gave such messages; this version shows its content",
			})
		}
		m.IdentityTerms = &terms
		return m
	}
	terms := identityTermsOf(v1)
	inventory := v1InventoryOf(v1)

	m, meta, site := fidelityContentOf(data)
	var regression, panicked string
	switch {
	case m == nil:
		panicked = "this version's parser failed while reading " + site
	default:
		regression = holdsV1(v1, inventory, m, meta)
		if regression == "" {
			keepV1Headers(m, v1)
		}
	}
	if m == nil || regression != "" {
		// Never worse than v1 (P13): v1's content, its display text valid UTF-8 (P7), and
		// a visible terminal gap saying why. The identity is unaffected: it is v1's.
		m = v1
		m.FellBack = true
		legacyBodies(m)
		detail := panicked
		if regression != "" {
			detail = "this version's reading of the message held less than the earlier parser's (" + regression + ")"
			m.ParseNotes = append(m.ParseNotes, newNote(noteFidelityRegression, regression))
		}
		m.ParseGaps = append(m.ParseGaps, model.ParseGap{
			Kind:   state.GapKindFidelityFallback,
			Detail: detail + "; the message is kept as an earlier version read it",
		})
	}
	m.TransportHeaders = headerBlock(data)
	m.V1Inventory = inventory
	m.IdentityTerms = &terms
	return m
}

// walkFault is a fault-injection seam (InjectParsePanics): when set and it accepts the
// message, the fidelity walk panics, as the frozen v1 parse does, so a message injected to
// panic is a raw stub whichever parser is active.
var walkFault func(data []byte) bool

// fidelityContentOf runs the content parser under its own recover. It returns a nil
// message when it panicked, and site names where the walker was (a bounded phrase, never
// a Go stack).
func fidelityContentOf(data []byte) (m *model.Message, meta walkMeta, site string) {
	w := newWalker(data)
	defer func() {
		if recover() != nil {
			m, site = nil, w.site
		}
	}()
	if parseContent != nil {
		return parseContent(data), walkMeta{}, ""
	}
	if walkFault != nil && walkFault(data) {
		panic("injected panic in the fidelity walk")
	}
	m, meta = w.run()
	return m, meta, ""
}

// legacyBodies makes the body text of v1's message valid UTF-8 where it is not, by the
// rule of every other body (util.DecodeLegacy): the fallback path shows text as clean as
// the walker's (P7, MC19).
func legacyBodies(m *model.Message) {
	for _, f := range []*string{&m.HTMLBody, &m.PlainBody} {
		if !utf8.ValidString(*f) {
			*f = util.DecodeLegacy([]byte(*f))
		}
	}
}

// keepV1Headers makes a header field the walker read as empty the one v1 read: the walker
// reads the envelope with the strict reader, and v1 read some messages (a Message-ID with
// 8-bit bytes, through its header fallback) more leniently, so the walker alone would show
// less than v1 and nothing in the check would see it (MS2-AC7). Where the walker read a
// value it stands.
func keepV1Headers(m, v1 *model.Message) {
	for _, f := range []struct{ now, v1 *string }{
		{&m.InternetMessageID, &v1.InternetMessageID}, {&m.Subject, &v1.Subject},
		{&m.SenderName, &v1.SenderName}, {&m.SenderEmail, &v1.SenderEmail},
		{&m.To, &v1.To}, {&m.Cc, &v1.Cc}, {&m.Bcc, &v1.Bcc}, {&m.ReplyTo, &v1.ReplyTo},
		{&m.InReplyTo, &v1.InReplyTo}, {&m.References, &v1.References},
	} {
		if *f.now == "" && *f.v1 != "" {
			*f.now = *f.v1
		}
	}
}

// bodyDecodingNotes are the notes that say a body's text was read differently from v1's:
// a body they touch need not hold v1's verbatim.
var bodyDecodingNotes = map[string]bool{
	noteCharsetFallback: true, noteLegacy8bit: true, noteHeaderFallback: true, noteLenientCTE: true,
	state.GapKindUndecodedPart: true, state.GapKindCorruptPart: true, state.GapKindUnsplitMultipart: true,
	noteContinuedAfterError: true,
}

// holdsV1 is the check "fidelity contains v1" (design §4.2 step 3a, MC4): every
// attachment v1 archived with bytes has an attachment here with identical bytes (a text
// attachment v1 converted to UTF-8 must convert, through v1's own table, to v1's bytes),
// and every body v1 had is not lost: a non-empty v1 body field has a non-empty one here,
// and one that is valid UTF-8 and was not read differently by design is contained
// verbatim. It returns "" when it holds, else what failed, in a short phrase.
func holdsV1(v1 *model.Message, inv []model.V1Item, m *model.Message, meta walkMeta) string {
	type content struct {
		size int64
		sum  [32]byte
	}
	have := make([]content, len(m.Attachments))
	used := make([]bool, len(m.Attachments))
	for i, a := range m.Attachments {
		if a.WriteTo == nil {
			continue
		}
		h := sha256.New()
		n, err := a.WriteTo(h)
		if err != nil {
			continue
		}
		have[i].size = n
		copy(have[i].sum[:], h.Sum(nil))
	}
	for _, it := range inv {
		if it.Size == 0 {
			continue
		}
		matched := false
		for i := range have {
			if !used[i] && have[i].size == it.Size && have[i].sum == it.SHA256 {
				used[i], matched = true, true
				break
			}
		}
		for i := 0; !matched && i < len(have); i++ {
			label := meta.declared[i]
			if used[i] || label == "" {
				continue
			}
			var buf bytes.Buffer
			if _, err := m.Attachments[i].WriteTo(&buf); err != nil {
				continue
			}
			r, err := v1charset.Reader(strings.ToLower(label), &buf)
			if err != nil {
				continue
			}
			conv, err := io.ReadAll(r)
			if err == nil && int64(len(conv)) == it.Size && sha256.Sum256(conv) == it.SHA256 {
				used[i], matched = true, true
			}
		}
		if !matched {
			return fmt.Sprintf("the attachment %s (%d bytes) is not in it", quoteLabel(it.Label), it.Size)
		}
	}

	differs := false
	for _, n := range m.ParseNotes {
		differs = differs || bodyDecodingNotes[n.Code]
	}
	for _, f := range []struct{ name, v1, now string }{
		{"HTML body", v1.HTMLBody, m.HTMLBody},
		{"plain body", v1.PlainBody, m.PlainBody},
		{"RTF body", v1.RTFBody, m.RTFBody},
	} {
		switch {
		case strings.TrimSpace(f.v1) != "" && strings.TrimSpace(f.now) == "":
			return "its " + f.name + " is empty"
		case !differs && f.v1 != "" && utf8.ValidString(f.v1) && !strings.Contains(f.now, f.v1):
			return "its " + f.name + " does not hold the earlier text"
		}
	}
	return ""
}
