package source

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-message/textproto"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// A walker that finds delimiter lines differently from the multipart reader cuts parts out
// of places the reader never sees, or misses the ones it does (MS2-AO3's second round). These
// tests hold the scan to the reader's own rule: a delimiter after the first one is a line that
// follows the container's line break (CRLF, or LF when the first delimiter ended in one), or
// the start of a part's empty body.

// covers: MA-349, R10
// MS2-AO3 (round 2). A line that looks like a delimiter but follows a bare LF in a CRLF
// message is not a delimiter to the multipart reader, so it must not be one to the scan:
// counted, it shifts the ordinals of the parts after it, the oversized part header is no
// longer cut before the reader (the walker's peak is the unfixed 120 times the message) and,
// when the reader's count catches the stale ordinal, a sibling is read twice.
func TestAPhantomDelimiterLineDoesNotHideAnOversizedPartHeader(t *testing.T) {
	useFidelity(t)
	padded := "--bd\r\n" + strings.Repeat("X:\r\n", 1<<20) + "Content-Type: text/plain\r\n\r\nthe padded part\r\n"
	sibling := "--bd\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"sibling.pdf\"\r\n\r\nthe sibling\r\n--bd--\r\n"

	for _, tc := range []struct{ name, phantom string }{
		{"a delimiter line after a bare LF", "first part\n--bd\r\nnot a delimiter for the reader\r\n"},
		{"a closing line after a bare LF", "first part\n--bd--\r\nnot the end for the reader\r\n"},
		{"a delimiter line with trailing white space after a bare LF", "first part\n--bd \t\r\nnot a delimiter\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := hostile(`multipart/mixed; boundary="bd"`, "--bd\r\nContent-Type: text/plain\r\n\r\n"+tc.phantom+padded+sibling)
			var m *model.Message
			peak := peakHeapOf(func() { m = parseFidelity(data) })
			t.Logf("message %d KiB, walker peak %d KiB (x%.1f)", len(data)>>10, peak>>10, float64(peak)/float64(len(data)))
			if limit := 2 * uint64(len(data)); peak > limit {
				t.Errorf("peak heap %d KiB is %.1f times the %d KiB message; want at most 2 times (the oversized part header was read whole)",
					peak>>10, float64(peak)/float64(len(data)), len(data)>>10)
			}
			names := attNames(m)
			if a, ok := attNamed(m, "part-2.undecoded"); !ok || a.GapKind != state.GapKindStructureLimit || !bytes.Contains(bytesOf(t, a), []byte("the padded part")) {
				t.Errorf("attachments %v, want part-2.undecoded (structure-limit) holding the padded part", names)
			}
			if len(names) != 2 || names[1] != "sibling.pdf" {
				t.Errorf("attachments %v, want exactly part-2.undecoded and sibling.pdf (a part read twice shows as a second blob)", names)
			}
			if s, ok := attNamed(m, "sibling.pdf"); !ok || string(bytesOf(t, s)) != "the sibling" {
				t.Errorf("the sibling of the oversized part: attachments %v", names)
			}
			if !strings.Contains(m.PlainBody, "first part") {
				t.Errorf("the first part was lost: %q", m.PlainBody)
			}
		})
	}

	t.Run("LF line ends", func(t *testing.T) {
		lf := []byte(strings.ReplaceAll(string(hostile(`multipart/mixed; boundary="bd"`,
			"--bd\r\nContent-Type: text/plain\r\n\r\nfirst part\r\n"+padded+sibling)), "\r\n", "\n"))
		var m *model.Message
		peak := peakHeapOf(func() { m = parseFidelity(lf) })
		if limit := 2 * uint64(len(lf)); peak > limit {
			t.Errorf("LF: peak heap %d KiB is %.1f times the %d KiB message; want at most 2 times (the scan did not read LF delimiters)",
				peak>>10, float64(peak)/float64(len(lf)), len(lf)>>10)
		}
		if names := attNames(m); len(names) != 2 || names[0] != "part-2.undecoded" || names[1] != "sibling.pdf" {
			t.Errorf("LF: attachments %v, want part-2.undecoded and sibling.pdf", names)
		}
	})

	t.Run("a first delimiter that ends in a bare LF", func(t *testing.T) {
		// The reader then takes only LF as a line break, and a CRLF delimiter line is not a delimiter
		// to it: it gives one part and stops at the line it cannot place. The rest is kept as
		// received, and the scan cuts no part out of it.
		data := hostile(`multipart/mixed; boundary="bd"`, "--bd\nContent-Type: text/plain\n\nfirst part\r\n"+padded+sibling)
		var m *model.Message
		peak := peakHeapOf(func() { m = parseFidelity(data) })
		if limit := 8 * uint64(len(data)); peak > limit {
			t.Errorf("mixed: peak heap %d KiB is %.1f times the message", peak>>10, float64(peak)/float64(len(data)))
		}
		names := attNames(m)
		if len(names) != 1 || names[0] != "remaining-parts.undecoded" {
			t.Fatalf("mixed: attachments %v, want only remaining-parts.undecoded (the part the scan cut is part-1.undecoded)", names)
		}
		if a := m.Attachments[0]; a.GapKind != state.GapKindCorruptPart || !bytes.Contains(bytesOf(t, a), []byte("the padded part")) || !bytes.Contains(bytesOf(t, a), []byte("the sibling")) {
			t.Errorf("mixed: the rest is %q (%s), want the padded part and the sibling as received", bytesOf(t, a)[:min(40, len(bytesOf(t, a)))], a.GapKind)
		}
	})
}

// randomContainer is a hostile multipart body of lines that are or nearly are delimiters,
// built from pieces of every ending, so the differential below meets the shapes a scan can
// get wrong.
func randomContainer(r *rand.Rand) []byte {
	pieces := []string{
		"--bd\r\n", "--bd\n", "--bd--\r\n", "--bd--\n", "--bd--", "--bd \r\n", "--bd\t\n", "--bd \t\r\n", "--bd\r", "--bd",
		"--bdx\r\n", "--bd-\r\n", "--bd-x\r\n", "-", "--", "\r\n", "\n", "\r", "\r\r\n",
		"X: y\r\n", "X: y\n", "Content-Type: text/plain\r\n", "Content-Type: text/plain\n", " continued\r\n", "no colon here\r\n",
		"text", "line\r\n", "line\n", "preamble\r\n", "\r\n\r\n", "\n\n",
	}
	whole := []string{
		"--bd\r\nContent-Type: text/plain\r\n\r\nbody\r\n", "--bd\nContent-Type: text/plain\n\nbody\n",
		"--bd\r\n\r\nbody\r\n", "--bd\r\n\r\n", "--bd\r\nX: y\r\n\r\n",
	}
	var b strings.Builder
	for n := 2 + r.Intn(24); n > 0; n-- {
		if r.Intn(3) == 0 {
			b.WriteString(whole[r.Intn(len(whole))])
		} else {
			b.WriteString(pieces[r.Intn(len(pieces))])
		}
	}
	return []byte(b.String())
}

// scanAgrees says the scan's end for part j is where the reader's body ends in parent.
func scanAgrees(parent []byte, end int, body []byte) bool {
	n := len(body)
	return end >= n && end <= len(parent) && bytes.Equal(parent[end-n:end], body)
}

// covers: MA-349, R10
// MS2-AO3 (round 2). The delimiter scan and the multipart reader agree: over random
// containers of every line ending and near-delimiter, every part the reader gives that is
// followed by another part, or by the end of the multipart, ends where the scan says it
// does (the walker then keeps a window onto the parent; a disagreement costs a copy and,
// before a delimiter line was held to the reader's rule, cut parts out of the wrong place).
func TestTheDelimiterScanAgreesWithTheMultipartReader(t *testing.T) {
	r := rand.New(rand.NewSource(20261012))
	parts, disagreements := 0, 0
	for i := 0; i < 60000; i++ {
		body := randomContainer(r)
		sc := scanParts(body, "bd")
		mr := textproto.NewMultipartReader(bytes.NewReader(body), "bd")
		var bodies [][]byte
		clean := false
		for {
			p, err := mr.NextPart()
			if err != nil {
				clean = errors.Is(err, io.EOF)
				break
			}
			b, _ := io.ReadAll(p)
			bodies = append(bodies, b)
		}
		for j, b := range bodies {
			if j == len(bodies)-1 && !clean {
				continue // the reader stopped inside or after this part: where it ends is the error's
			}
			parts++
			if !scanAgrees(body, sc.end(body, j), b) {
				disagreements++
				if disagreements <= 5 {
					t.Errorf("part %d of %q: the reader's body is %q, the scan ends it at %d (%q)", j, body, b, sc.end(body, j), body[:min(sc.end(body, j), len(body))])
				}
			}
		}
		if len(sc.starts) < len(bodies) && len(sc.starts) <= maxParts {
			t.Errorf("%q: the reader gave %d parts and the scan found %d delimiter lines", body, len(bodies), len(sc.starts))
		}
	}
	t.Logf("%d parts compared", parts)
	if parts < 20000 {
		t.Fatalf("the differential compared only %d parts: the generator is not exercising the scan", parts)
	}
	if disagreements > 0 {
		t.Errorf("%d of %d parts end somewhere the reader does not end them", disagreements, parts)
	}
}

// nestedText is a message of levels nested multipart/mixed containers whose innermost holds
// one text/plain part of body.
func nestedText(levels int, body string) []byte {
	b := func(n int) string { return "b" + strconv.Itoa(n) }
	inner := "--" + b(levels) + "\r\nContent-Type: text/plain\r\n\r\n" + body + "\r\n--" + b(levels) + "--\r\n"
	for i := levels - 1; i >= 1; i-- {
		inner = "--" + b(i) + "\r\nContent-Type: multipart/mixed; boundary=\"" + b(i+1) + "\"\r\n\r\n" + inner + "\r\n--" + b(i) + "--\r\n"
	}
	return hostile(`multipart/mixed; boundary="b1"`, inner)
}

// covers: MA-349, R10
// MS2-AO3 (round 2), the cost of the fix. The scan visits every container's body once per
// nesting level, so it must not cost a step per line: a body of newline-only lines nested 31
// deep took 4 seconds at 16 MiB when the scan read it line by line (the same size of ordinary
// lines took 0.17 s). Hostile and ordinary bodies of one size must cost about the same.
func TestTheScanDoesNotCostAStepPerLineOnNestedNewlines(t *testing.T) {
	useFidelity(t)
	const size = 6 << 20
	ordinary := strings.Repeat(strings.Repeat("a", 75)+"\r\n", size/77)
	newlines := strings.Repeat("\n", size)
	best := func(body string) time.Duration {
		data := nestedText(31, body)
		var d time.Duration
		for i := 0; i < 2; i++ {
			start := time.Now()
			m := parseFidelity(data)
			if e := time.Since(start); i == 0 || e < d {
				d = e
			}
			if len(m.ParseGaps) != 0 || len(m.Attachments) != 0 {
				t.Fatalf("the nested message was not read: gaps %v, attachments %v", m.ParseGaps, attNames(m))
			}
		}
		return d
	}
	base, hostileD := best(ordinary), best(newlines)
	t.Logf("%d MiB nested 31 deep: ordinary lines %v, newline-only lines %v", size>>20, base, hostileD)
	if hostileD > 6*base+300*time.Millisecond {
		t.Errorf("newline-only lines took %v against %v for ordinary lines of the same size: the scan costs a step per line", hostileD, base)
	}
}

// covers: MA-337, MA-349, R1, R26
// MS2-AC3 (round 2). A nested multipart that lacks its own closing delimiter, inside a
// message that is otherwise complete, is not a message cut off: its last attachment is
// whole and keeps its name, and no fillable gap says it is damaged (the end of the nested
// body is the outer delimiter, not the end of the data). The same nested multipart at the
// end of data that is really cut is still kept as partial.
func TestANestedMultipartWithoutItsClosingLineIsNotACutMessage(t *testing.T) {
	useFidelity(t)
	pdf := randomBytes(3000)
	pdfPart := "--in\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"a.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\n" + base64Lines(pdf)
	nested := "--bd\r\nContent-Type: text/plain\r\n\r\ntext\r\n" +
		"--bd\r\nContent-Type: multipart/mixed; boundary=\"in\"\r\n\r\n--in\r\nContent-Type: text/plain\r\n\r\ninner text\r\n" + strings.TrimSuffix(pdfPart, "\r\n")
	tail := "\r\n--bd\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"b.pdf\"\r\n\r\nthe second file\r\n--bd--\r\n"

	t.Run("whole message", func(t *testing.T) {
		m := safeParseMessage(hostile(`multipart/mixed; boundary="bd"`, nested+tail))
		requireNoStub(t, "nested without its closing line", m)
		names := attNames(m)
		if a, ok := attNamed(m, "a.pdf"); !ok || !bytes.Equal(bytesOf(t, a), pdf) || a.GapKind != "" {
			t.Errorf("attachments %v, want a.pdf whole with no gap (the data did not end in it)", names)
		}
		if b, ok := attNamed(m, "b.pdf"); !ok || string(bytesOf(t, b)) != "the second file" {
			t.Errorf("attachments %v, want b.pdf", names)
		}
		for _, g := range m.ParseGaps {
			if g.Kind == state.GapKindCorruptPart {
				t.Errorf("gap %v: a complete attachment is called cut short or damaged", g)
			}
		}
	})

	t.Run("the same message cut off", func(t *testing.T) {
		m := safeParseMessage(hostile(`multipart/mixed; boundary="bd"`, nested))
		a, ok := attNamed(m, "a.pdf.partial")
		if !ok || a.GapKind != state.GapKindCorruptPart {
			t.Errorf("attachments %v, want a.pdf.partial (corrupt-part): the data ends in it", attNames(m))
		}
	})
}
