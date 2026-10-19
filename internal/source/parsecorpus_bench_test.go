package source

import (
	"encoding/base64"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// benchMessage is a message of one text part and one base64 attachment of n bytes of
// incompressible data (a fixed seed, so every run measures the same input).
func benchMessage(n int) []byte {
	data := make([]byte, n)
	rand.New(rand.NewSource(1)).Read(data)
	enc := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	b.WriteString("Message-ID: <bench@x.example>\r\nDate: Mon, 02 Jan 2023 15:04:05 +0000\r\nFrom: A <a@x.example>\r\nTo: b@x.example\r\nSubject: bench\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"bd\"\r\n\r\n" +
		"--bd\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nA body.\r\n" +
		"--bd\r\nContent-Type: application/octet-stream; name=\"a.bin\"\r\nContent-Disposition: attachment; filename=\"a.bin\"\r\nContent-Transfer-Encoding: base64\r\n\r\n")
	for len(enc) > 76 {
		b.WriteString(enc[:76])
		b.WriteString("\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc)
	b.WriteString("\r\n--bd--\r\n")
	return []byte(b.String())
}

// BenchmarkParseCorpus is the cost of what a raw-MIME message costs every run (design-
// mime-fidelity §4.2, MC39): the frozen v1 replay and the fidelity parse, with the
// "fidelity contains v1" check, at three sizes: the corpus (110 small messages, one
// iteration parses them all), a 1 MiB attachment and a 16 MiB one. Each size is also run
// with v1 alone, so the added cost of the walker is the difference. bytes/op is the
// message's size, so MB/s is read straight from the output.
func BenchmarkParseCorpus(b *testing.B) {
	var corpusMsgs [][]byte
	for _, it := range loadMimeCorpus(b) {
		corpusMsgs = append(corpusMsgs, it.data)
	}
	cases := []struct {
		name string
		msgs [][]byte
	}{
		{"corpus", corpusMsgs},
		{"1MiB", [][]byte{benchMessage(1 << 20)}},
		{"16MiB", [][]byte{benchMessage(16 << 20)}},
	}
	for _, c := range cases {
		size := 0
		for _, m := range c.msgs {
			size += len(m)
		}
		b.Run(fmt.Sprintf("%s/v1-only", c.name), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				for _, m := range c.msgs {
					replayV1(m)
				}
			}
		})
		b.Run(fmt.Sprintf("%s/replay+fidelity", c.name), func(b *testing.B) {
			old := fidelityContent
			fidelityContent = true
			defer func() { fidelityContent = old }()
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				for _, m := range c.msgs {
					safeParseMessage(m)
				}
			}
		})
	}
}

// covers: MA-349, R10
// The benchmark's own inputs parse on the path it means to measure: no fallback, no gap,
// every attachment byte kept, and the corpus has its 110 messages.
func TestParseCorpusBenchmarkInputsTakeTheFullPath(t *testing.T) {
	useFidelity(t)
	for _, n := range []int{1 << 20, 3 << 20} {
		m := safeParseMessage(benchMessage(n))
		if m.FellBack || len(m.ParseGaps) != 0 || len(m.Attachments) != 1 || len(bytesOf(t, m.Attachments[0])) != n {
			t.Errorf("a %d-byte message: fell back %t, gaps %v, %d attachments", n, m.FellBack, m.ParseGaps, len(m.Attachments))
		}
	}
	if items := loadMimeCorpus(t); len(items) < 100 {
		t.Errorf("the corpus holds %d messages", len(items))
	}
}
