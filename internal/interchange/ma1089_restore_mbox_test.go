package interchange

import (
	"bytes"
	"strings"
	"testing"
	"testing/iotest"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1089, R40
// The restore's mbox bytes (docs/design-restore-thunderbird.md §4.1–§4.3), against the
// product reader ReadMboxrd, a port of Thunderbird's read rules (MboxMsgInputStream:
// a "From " line at column 0 starts a message, one '>' comes off a `>*From ` line, the
// LF or CRLF blank line before the next "From " is the separator) — a model taken from
// its source, not Thunderbird (lab row MA-1093 is the row against Thunderbird). Every
// preserved byte comes back, in order: "From ", ">From " and ">>From " lines, a first
// line beginning "From ", CRLF and LF messages, a message ending in a blank line, an
// empty message, a final line with no newline (the writer adds the message's own line
// ending and says how many bytes, so the check takes exactly those off), and a line
// longer than the reader's buffer. The framing follows each message's own line ending,
// so a file never mixes them; extract's WriteMessage keeps its LF framing.
func TestRestoreMboxRoundTrip(t *testing.T) {
	long := "X-Long: " + strings.Repeat("a", 200<<10) + "\r\n"
	msgs := [][]byte{
		[]byte("From: Ann <ann@example.org>\r\nDate: Mon, 2 Nov 2026 10:00:00 +0000\r\n\r\nFrom here\r\n>From there\r\n>>From deep\r\nend\r\n"),
		[]byte("From: bob@example.org\n\nlf body\nFrom the start of a line\n"),
		[]byte("Subject: blank last line\r\n\r\nbody\r\n\r\n"),
		[]byte(""),
		[]byte("Subject: no final newline\r\n\r\nlast line"),
		[]byte("Subject: lf no final newline\n\nlast"),
		[]byte("From first line is a From line\nSubject: odd\n\nx\n"),
		[]byte(long + "\r\nbody\r\n"),
		[]byte(">From quoted first\n\n"),
	}
	var file bytes.Buffer
	var added []int
	for _, m := range msgs {
		a, err := WriteMessageMatched(&file, m)
		if err != nil {
			t.Fatal(err)
		}
		added = append(added, a)
	}
	if added[4] != 2 || added[5] != 1 || added[0] != 0 || added[3] != 0 {
		t.Fatalf("added line endings: %v", added)
	}
	// The framing follows each message's own line ending.
	lines := strings.SplitAfter(file.String(), "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "From ann@example.org ") && !strings.HasSuffix(l, "\r\n") {
			t.Errorf("a CRLF message's From line ends in CRLF: %q", l)
		}
		if strings.HasPrefix(l, "From bob@example.org ") && strings.HasSuffix(l, "\r\n") {
			t.Errorf("an LF message's From line ends in LF: %q", l)
		}
	}
	for _, r := range []func([]byte) *bytes.Reader{func(b []byte) *bytes.Reader { return bytes.NewReader(b) }} {
		var got [][]byte
		err := ReadMboxrd(iotest.OneByteReader(r(file.Bytes())), func(b []byte) error {
			got = append(got, append([]byte(nil), b...))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(assure.Reached(t, got, "messages read back")) != len(msgs) {
			t.Fatalf("%d messages read back, %d written", len(got), len(msgs))
		}
		for i := range msgs {
			back := got[i][:len(got[i])-added[i]]
			if !bytes.Equal(back, msgs[i]) {
				t.Errorf("message %d: got %q, want %q", i, clip(back), clip(msgs[i]))
			}
		}
	}

	// Thunderbird's rules on a file not written by us: no blank line before a From line,
	// lines before the first From line ignored.
	hand := "preamble\nFrom a\nSubject: 1\n\nb1\nFrom b\nSubject: 2\n\n>From q\n"
	var got []string
	if err := ReadMboxrd(strings.NewReader(hand), func(b []byte) error { got = append(got, string(b)); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "Subject: 1\n\nb1\n" || got[1] != "Subject: 2\n\nFrom q\n" {
		t.Fatalf("hand-made mbox: %q", got)
	}

	// Extract's writer is unchanged: LF framing around a CRLF message.
	var ex bytes.Buffer
	if err := WriteMessage(&ex, msgs[0]); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ex.String(), "From ann@example.org Mon Nov  2 10:00:00 2026\n") || !strings.HasSuffix(ex.String(), "end\r\n\n") {
		t.Fatalf("extract's framing changed: %q", clip(ex.Bytes()))
	}
}

func clip(b []byte) string {
	if len(b) > 120 {
		return string(b[:120]) + "…"
	}
	return string(b)
}
