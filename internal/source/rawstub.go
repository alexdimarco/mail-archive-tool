package source

import (
	"strings"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// The raw-preserving stub (design-fault-isolation §4.5, F33, FC25). A message whose
// MIME parse gave up is not an empty placeholder any more: it keeps its bytes (Raw,
// which -raw writes as the .eml), its page and its index text carry the start of those
// bytes as escaped text, and it is labelled unparsed, terminal for this parser
// generation. Its IDENTITY is still today's constant stub (stubIdentityTerms): the fold
// of the raw bytes that would tell two such messages apart moves MA-330's identity
// golden, so it waits for the owner's answer to ESC-1 (rulings J5 and J8, slice
// FI-S2b-id).

// stubTextLimit bounds the text a stub's page and index carry: the first 64 KiB of the
// bytes, which holds the header block, since a header block is read no further than
// that (headerBlock). A hostile message cannot bloat the index through its stub.
const stubTextLimit = 64 << 10

// stubNote ends every stub's text: a fixed sentence, so the reader of the page knows
// what it is looking at and where the whole message is.
const stubNote = "\n\n[mailarchive could not parse this message, so this page shows the start of its raw bytes as text (at most the first 64 KiB). The whole message is kept as the .eml beside this page when the archive is made with -raw.]"

// stubText is the bounded text of a raw stub: the start of data, cut on a rune
// boundary, with every control character but a line break or a tab stripped (a NUL
// would end a string in a consumer written in C, an escape would reach a terminal) and
// invalid UTF-8 replaced, then the fixed note.
func stubText(data []byte) string {
	if len(data) > stubTextLimit {
		data = data[:stubTextLimit]
	}
	text := strings.ToValidUTF8(string(data), "�")
	text = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r == '\r':
			return -1
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return -1
		}
		return r
	}, text)
	return text + stubNote
}

// parseRaw is how every raw-bytes reader (mbox, maildir, Evolution, and Graph through
// ParseRFC822) parses a message: safeParseMessage, and the raw-preserving stub for a
// message the parse panicked on, which safeParseMessage leaves as today's empty
// constant stub (its identity, MA-330) with nothing to keep.
func parseRaw(data []byte) *model.Message {
	m := safeParseMessage(data)
	if m != nil && isPanicStub(m) {
		return rawStub(m, data)
	}
	return m
}

// isPanicStub says m is the stub safeParseMessage returns for a panic: every parsed
// message carries its Raw bytes, the stub does not, so a real message that merely
// shares the stub's subject is not mistaken for it.
func isPanicStub(m *model.Message) bool {
	return m.Raw == nil && m.Subject == stubSubject && m.IdentityTerms != nil
}

// rawStub gives the stub its bytes and its label. The identity terms stay the
// constant stub's.
func rawStub(m *model.Message, data []byte) *model.Message {
	m.Raw = data
	m.TransportHeaders = headerBlock(data)
	m.PlainBody = stubText(data)
	m.ReadGaps = []string{state.LabelUnparsed}
	m.ReadError = "the message could not be parsed"
	return m
}

// InjectParsePanics makes the frozen v1 parse panic on every message match accepts,
// until restore is called: no input is known that does, so the raw-preserving stub of a
// MIME-panicking message is exercised through this fault-injection seam (the same use
// of v1Parse the package's own tests make). It is for tests; nothing in production
// calls it.
func InjectParsePanics(match func(data []byte) bool) (restore func()) {
	old := v1Parse
	v1Parse = func(data []byte) *model.Message {
		if match(data) {
			panic("injected panic in the v1 parse")
		}
		return old(data)
	}
	return func() { v1Parse = old }
}
