package interchange

import (
	"bytes"
	"net/mail"
	"strings"
	"testing"
)

// splitMboxrd reverses WriteMessage's framing for a test consumer, operating on
// bytes so line endings are preserved exactly (a bufio.Scanner would strip
// '\r'): it splits the stream on "From " postmark lines (column 0 — a body
// From-line is always '>'-quoted, so it can never match), drops the single blank
// line separating each message from the next postmark, and un-quotes `>+From `
// body lines (removing one leading '>'). It returns each recovered message's raw
// bytes.
func splitMboxrd(stream []byte) [][]byte {
	// Postmark start offsets: index 0 (if the stream opens with "From ") and any
	// index right after a '\n' whose line begins "From ".
	var starts []int
	if bytes.HasPrefix(stream, []byte("From ")) {
		starts = append(starts, 0)
	}
	for i := 0; i+1 < len(stream); i++ {
		if stream[i] == '\n' && bytes.HasPrefix(stream[i+1:], []byte("From ")) {
			starts = append(starts, i+1)
		}
	}
	var msgs [][]byte
	for si, s := range starts {
		// Skip the postmark line itself (up to and including its '\n').
		nl := bytes.IndexByte(stream[s:], '\n')
		if nl < 0 {
			continue
		}
		blockStart := s + nl + 1
		blockEnd := len(stream)
		if si+1 < len(starts) {
			blockEnd = starts[si+1]
		}
		block := stream[blockStart:blockEnd]
		// The block is the message content (each line newline-terminated) plus one
		// blank-line separator: strip exactly one trailing '\n'.
		block = bytes.TrimSuffix(block, []byte("\n"))
		lines := bytes.Split(block, []byte("\n"))
		for i, ln := range lines {
			if isFromQuoteLine(ln) && len(ln) > 0 && ln[0] == '>' {
				lines[i] = ln[1:]
			}
		}
		msgs = append(msgs, bytes.Join(lines, []byte("\n")))
	}
	return msgs
}

// covers: MA-181, R20, S34
// The mboxrd writer frames each message with a synthesized "From " postmark and
// >-quotes any `>*From ` body line, so a standard reader that un-quotes recovers
// the exact message bytes and the boundaries survive: three messages in, three
// messages out, with a body "From " line and a ">From" line preserved.
func TestMboxrdRoundTripAndFromQuoting(t *testing.T) {
	m1 := "From: alice@example.com\r\nTo: bob@example.com\r\nSubject: One\r\n" +
		"Date: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\n" +
		"Hello.\r\nFrom here you can see the sea.\r\n>From a quoted original.\r\n"
	m2 := "From: carol@example.com\r\nSubject: Two\r\n\r\nJust a body.\r\n"
	m3 := "From: dave@example.com\r\nSubject: Three\r\n\r\nFrom the start, no headers to fold.\r\n"
	raws := [][]byte{[]byte(m1), []byte(m2), []byte(m3)}

	var buf bytes.Buffer
	for _, r := range raws {
		if err := WriteMessage(&buf, r); err != nil {
			t.Fatalf("WriteMessage: %v", err)
		}
	}
	stream := buf.Bytes()

	// A body "From " line must be >-quoted, or a reader would see a false
	// boundary. The writer emitted ">From here…" and ">>From a quoted…".
	if !strings.Contains(string(stream), "\n>From here you can see the sea.") {
		t.Errorf("body 'From ' line was not >-quoted:\n%s", stream)
	}
	if !strings.Contains(string(stream), "\n>>From a quoted original.") {
		t.Errorf("body '>From ' line was not >-quoted:\n%s", stream)
	}

	got := splitMboxrd(stream)
	if len(got) != len(raws) {
		t.Fatalf("round-trip message count = %d, want %d\n%s", len(got), len(raws), stream)
	}
	for i, g := range got {
		// The exact original bytes are recovered (line endings preserved).
		if !bytes.Equal(g, raws[i]) {
			t.Errorf("message %d not recovered:\n got %q\nwant %q", i, g, raws[i])
		}
		msg, err := mail.ReadMessage(bytes.NewReader(g))
		if err != nil {
			t.Fatalf("message %d does not parse as RFC 822: %v\n%q", i, err, g)
		}
		if msg.Header.Get("Subject") == "" {
			t.Errorf("message %d lost its Subject header after round-trip: %q", i, g)
		}
	}
}

// covers: MA-181, R20, S34
// The postmark is synthesized deterministically from the message's own From/Date
// headers (a re-run produces identical bytes), and falls back to a stable
// placeholder for a header-less or unparseable message rather than failing (R10).
func TestPostmarkDeterministicWithFallback(t *testing.T) {
	withHeaders := []byte("From: alice@example.com\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\nbody\r\n")
	a := Postmark(withHeaders)
	b := Postmark(withHeaders)
	if a != b {
		t.Errorf("postmark not deterministic: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "From alice@example.com ") {
		t.Errorf("postmark did not carry the From address: %q", a)
	}
	// Header-less garbage still yields a valid, single-line "From " postmark.
	fb := Postmark([]byte("this is not a message"))
	if !strings.HasPrefix(fb, "From ") || strings.ContainsAny(fb, "\r\n") {
		t.Errorf("fallback postmark is not a clean 'From ' line: %q", fb)
	}
	if !strings.Contains(fb, "MAILER-DAEMON") {
		t.Errorf("fallback postmark should use the MAILER-DAEMON placeholder: %q", fb)
	}
}
