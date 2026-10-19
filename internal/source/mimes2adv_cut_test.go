package source

import (
	"bytes"
	"encoding/base64"
	"math/rand"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// base64Lines is data as base64 in 76-character CRLF lines (a whole number of quanta per line).
func base64Lines(data []byte) string {
	enc := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	for i := 0; i < len(enc); i += 76 {
		b.WriteString(enc[i:min(i+76, len(enc))])
		b.WriteString("\r\n")
	}
	return b.String()
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(int64(n))).Read(b)
	return b
}

const cutPrefix = "--bd\r\nContent-Type: text/plain\r\n\r\nhello\r\n"

// covers: MA-337, MA-349, R1, R26
// MS2-AC3. A part the data ends in the middle of is never stored under its real name: a
// message or attachment cut off on a line boundary (a base64 quantum boundary, so the
// decoder sees nothing wrong), a 7bit or 8bit part, and a text body are all cut short, and
// are kept as <name>.partial with a corrupt-part gap (MA-337's "a truncated attachment is
// never stored under its real name", design 4.3 steps 3 and 4). The twin, the same
// message with its closing delimiter, is read as ever.
func TestAPartTheDataEndsInIsKeptAsPartialNotAsComplete(t *testing.T) {
	useFidelity(t)
	pdf := randomBytes(3000)
	head := "--bd\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"a.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\n"
	lines := strings.SplitAfter(base64Lines(pdf), "\r\n")
	cutB64 := strings.Join(lines[:20], "") // 20 whole lines: 1,140 bytes, no closing delimiter

	t.Run("base64 attachment cut on a line boundary", func(t *testing.T) {
		m := safeParseMessage(hostile(`multipart/mixed; boundary="bd"`, cutPrefix+head+cutB64))
		requireNoStub(t, "cut base64", m)
		a, ok := attNamed(m, "a.pdf.partial")
		if !ok || a.GapKind != state.GapKindCorruptPart || !bytes.Equal(bytesOf(t, a), pdf[:1140]) {
			t.Fatalf("attachments %v, want a.pdf.partial (corrupt-part) holding the 1,140 bytes that arrived", attNames(m))
		}
		if _, ok := attNamed(m, "a.pdf"); ok {
			t.Errorf("a cut attachment is stored under its real name: %v", attNames(m))
		}
		if m.FellBack {
			t.Errorf("the message fell back to v1: %v", m.ParseGaps)
		}
	})

	t.Run("the same message whole", func(t *testing.T) {
		m := safeParseMessage(hostile(`multipart/mixed; boundary="bd"`, cutPrefix+head+strings.Join(lines, "")+"--bd--\r\n"))
		a, ok := attNamed(m, "a.pdf")
		if !ok || !bytes.Equal(bytesOf(t, a), pdf) || len(m.ParseGaps) != 0 || len(m.Attachments) != 1 || m.FellBack {
			t.Errorf("attachments %v gaps %v fell back %t, want a.pdf whole and no gap", attNames(m), m.ParseGaps, m.FellBack)
		}
	})

	t.Run("a 7bit attachment cut off", func(t *testing.T) {
		m := safeParseMessage(hostile(`multipart/mixed; boundary="bd"`,
			cutPrefix+"--bd\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=\"q.bin\"\r\n\r\nthe first half of a file"))
		a, ok := attNamed(m, "q.bin.partial")
		if !ok || a.GapKind != state.GapKindCorruptPart || string(bytesOf(t, a)) != "the first half of a file" {
			t.Errorf("attachments %v, want q.bin.partial (corrupt-part)", attNames(m))
		}
	})

	t.Run("a body text cut off", func(t *testing.T) {
		m := safeParseMessage(hostile(`multipart/mixed; boundary="bd"`, "--bd\r\nContent-Type: text/plain\r\n\r\nthe start of a long message"))
		if !strings.Contains(m.PlainBody, "the start of a long message") {
			t.Errorf("the text that arrived is not shown: %q", m.PlainBody)
		}
		found := false
		for _, g := range m.ParseGaps {
			found = found || g.Kind == state.GapKindCorruptPart
		}
		if !found {
			t.Errorf("gaps %v, want a corrupt-part gap for the cut text", m.ParseGaps)
		}
	})
}
