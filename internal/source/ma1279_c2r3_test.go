package source

// The C2 adversarial pass round 3's fixes in the walk's count
// (docs/review-thunderbird-completeness-c2-adversarial-r3.md: R3-01, and the held R3-02 and R42
// shapes). The round's attack tests are brought in from its clone (.claude/attack-c2-r3,
// internal/source/zz_r3_test.go); its round-1 helper r1CountOnly is this package's countOnly.

import (
	"bytes"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	gombox "github.com/emersion/go-mbox"
)

// covers: MA-1279
// R1's ADV-C2-13 adapted to R2-04 (brought in): the fixture's pieces carry no X-Mozilla-Status, so
// they are Loose; the bound must still hold (2,001 loose, 2,000 keys, the one past the bound
// unkeyed).
func TestR3_R1_13_Adapted(t *testing.T) {
	big := filepath.Join(t.TempDir(), "imap.big.example")
	var b strings.Builder
	b.WriteString("From - Mon Mar  3 09:00:00 2025\n")
	for b.Len() < 4<<20 {
		b.WriteString("X-Junk: " + strings.Repeat("a", 70) + "\n")
	}
	b.WriteString("Message-ID: <late@x>\n\nbody\n\n")
	line := "X-H: " + strings.Repeat("b", 1000) + "\n"
	for i := 0; i < 2000; i++ {
		b.WriteString("From - Mon Mar  3 09:00:00 2025\n")
		b.WriteString(strings.Repeat(line, 60))
		b.WriteString("Message-ID: <m" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + strconv.Itoa(i) + "@x>\n\nbody\n\n")
	}
	countWrite(t, filepath.Join(big, "INBOX"), b.String())
	g, _ := countOnly(t, big, true)
	mb := g["INBOX"]
	if mb.Messages != 0 || mb.Loose != 2001 || len(mb.LooseKeys) != 2000 || mb.LooseNoKey != 1 || mb.Misaligned {
		t.Errorf("strict %d loose %d loose keys %d loose no-key %d misaligned %v", mb.Messages, mb.Loose, len(mb.LooseKeys), mb.LooseNoKey, mb.Misaligned)
	}
}

// r3Long is a body line starting "From " whose bytes past go-mbox's 4096-byte line buffer begin
// "From " again: go-mbox ends a piece at the line's first chunk and, with a fresh reader that does
// not know it is mid-line, ends another at the second chunk. separatorScan notes one line.
func r3Long() string {
	head := "From x"
	return head + strings.Repeat("x", 4096-len(head)) + "From y\n"
}

// r3File is R3-01's mailbox: three real Thunderbird messages (a, b, c), with a long "From " body
// line (escaped as Thunderbird writes it when escape is ">", unescaped when "") in b, followed by a
// crafted block carrying an expunged X-Mozilla-Status.
func r3File(escape string) string {
	sep := "From - Mon Mar  3 09:00:00 2025\n"
	msg := func(id string) string {
		return "X-Mozilla-Status: 0001\nMessage-ID: <" + id + "@x>\nSubject: s\n\nbody\n\n"
	}
	return sep + msg("a") + sep + "X-Mozilla-Status: 0001\nMessage-ID: <b@x>\n\nbody\n\n" + escape + r3Long() +
		"X-Mozilla-Status: 0009\nMessage-ID: <crafted@x>\n\nhidden\n\n" + sep + msg("c")
}

// covers: MA-1279
// R3-01 (brought in; the effect adapted to the lead's call). The mechanism, unchanged: go-mbox
// gives one piece more than the scan notes for a "From " line longer than its buffer. The effect
// asserted then was "the lenient count is at least 3"; the lead's call instead never matches a
// piece to another piece's separator: when the two numbers differ the mailbox is Misaligned,
// nothing of it is counted (so nothing is short toward a false behind), and every piece is still
// archived. Positive twin first: the same mailbox with the long line escaped, as Thunderbird
// writes it, counts a, b and c for certain.
func TestR3_01_LongFromLineShiftsTheNotes(t *testing.T) {
	twin := filepath.Join(t.TempDir(), "imap.c2.example")
	countWrite(t, filepath.Join(twin, "INBOX"), r3File(">"))
	if got, _ := countOnly(t, twin, true); got["INBOX"].Messages != 3 || got["INBOX"].Misaligned || got["INBOX"].Unseparated != 0 || len(got["INBOX"].Keys) != 3 {
		t.Fatalf("positive twin: %+v", got["INBOX"])
	}

	file := r3File("")
	// Mechanism: count go-mbox's pieces and the scan's notes on the same bytes.
	scan := newSeparatorScan(bytes.NewReader([]byte(file)))
	mr := gombox.NewReader(scan)
	pieces := 0
	for {
		r, err := mr.NextMessage()
		if err != nil {
			break
		}
		io.Copy(io.Discard, r)
		pieces++
	}
	if pieces != len(scan.marks)+1 {
		t.Fatalf("the trigger: go-mbox gave %d pieces, the scan noted %d lines (want one more piece)", pieces, len(scan.marks))
	}
	// Effect: the walk sees the numbers differ, matches nothing, and archives every piece.
	dir := filepath.Join(t.TempDir(), "imap.c2.example")
	countWrite(t, filepath.Join(dir, "INBOX"), file)
	got, archived := countOnly(t, dir, true)
	mb := got["INBOX"]
	if !mb.Misaligned || mb.Uncounted != int64(pieces) || archived != pieces {
		t.Errorf("pieces and notes differ: misaligned %v, uncounted %d, archived %d (want true, %d, %d)", mb.Misaligned, mb.Uncounted, archived, pieces, pieces)
	}
	if mb.Messages != 0 || mb.Loose != 0 || mb.Expunged != 0 || mb.Unseparated != 0 || mb.Glued != 0 || len(mb.Keys) != 0 || len(mb.LooseKeys) != 0 || mb.NoKey != 0 || mb.LooseNoKey != 0 {
		t.Errorf("a piece was matched to a separator in a misaligned mailbox: %+v", mb)
	}
}

// covers: MA-1279
// R3-01, the rule swept over where the chunks fall: a "From " line whose length puts a later
// "From " near or at a multiple of 4,096 bytes, one, two or three chunks long, with a CR before
// its LF or not. Whatever go-mbox does with it, the count never depends on it: either the numbers
// agree and the three real messages are each counted (a, c for certain, b's piece then holds the
// long line), or the mailbox is Misaligned and nothing is counted. Never fewer than 3 counted in an
// aligned mailbox (a false behind), never a misaligned one with a count. Positive twin: a long line
// that does not start "From " is an ordinary body line, 3 for certain.
func TestTheCountNeverDependsOnWhereTheChunksFall(t *testing.T) {
	sep := "From - Mon Mar  3 09:00:00 2025\n"
	msg := func(id, body string) string {
		return "X-Mozilla-Status: 0001\nMessage-ID: <" + id + "@x>\n\n" + body + "\n\n"
	}
	twin := filepath.Join(t.TempDir(), "imap.c2.example")
	countWrite(t, filepath.Join(twin, "INBOX"), sep+msg("a", "x")+sep+msg("b", strings.Repeat("y", 9000)+"From z")+sep+msg("c", "x"))
	if got, _ := countOnly(t, twin, false); got["INBOX"].Messages != 3 || got["INBOX"].Misaligned {
		t.Fatalf("positive twin: %+v", got["INBOX"])
	}
	misaligned, aligned := 0, 0
	for _, chunks := range []int{1, 2, 3} {
		for off := -3; off <= 3; off++ {
			for _, cr := range []string{"", "\r"} {
				n := chunks*4096 + off
				long := "From x" + strings.Repeat("x", max(0, n-len("From x"))) + "From y" + cr + "\n"
				file := sep + msg("a", "x") + sep + "X-Mozilla-Status: 0001\nMessage-ID: <b@x>\n\nbody\n\n" + long + "\n" + sep + msg("c", "x")
				dir := filepath.Join(t.TempDir(), "imap.c2.example")
				countWrite(t, filepath.Join(dir, "INBOX"), file)
				got, archived := countOnly(t, dir, false)
				mb := got["INBOX"]
				counted := mb.Messages + mb.Loose + mb.Expunged + mb.Unseparated + mb.Glued
				switch {
				case mb.Misaligned:
					misaligned++
					if counted != 0 || mb.Uncounted != int64(archived) {
						t.Errorf("chunks %d off %d cr %q: misaligned with a count %+v (archived %d)", chunks, off, cr, mb, archived)
					}
				default:
					aligned++
					if mb.Messages+mb.Loose < 3 || counted != int64(archived) {
						t.Errorf("chunks %d off %d cr %q: aligned, %+v of %d pieces: short toward a false behind", chunks, off, cr, mb, archived)
					}
				}
			}
		}
	}
	if misaligned == 0 || aligned == 0 {
		t.Errorf("the sweep reached %d misaligned and %d aligned mailboxes; want both", misaligned, aligned)
	}
}

// covers: MA-1279
// R3-02 (brought in, held; the residual R2-04 accepts by design): a body line of exactly
// Thunderbird's separator after an empty line, not escaped, followed by an X-Mozilla-Status line,
// is counted STRICT: one real message counts 2. Thunderbird escapes body "From " lines (the owner's
// profile: 0 bare, 5,655 ">From "), so only a file another writer made can carry it.
func TestR3_02_BodySeparatorWithStatusIsStrict(t *testing.T) {
	sep := "From - Mon Mar  3 09:00:00 2025\n"
	file := sep + "X-Mozilla-Status: 0001\nMessage-ID: <real@x>\n\nbody\n\n" + sep + "X-Mozilla-Status: 0001\nMessage-ID: <fake@x>\n\nstill the real one's body\n"
	dir := filepath.Join(t.TempDir(), "imap.c2.example")
	countWrite(t, filepath.Join(dir, "INBOX"), file)
	got, _ := countOnly(t, dir, false)
	if got["INBOX"].Messages != 2 {
		t.Fatalf("the residual's shape: strict %d", got["INBOX"].Messages)
	}
	// The same with an empty header (the separator then an empty line): the "header" the strict
	// count reads is the body's first block.
	file2 := sep + "X-Mozilla-Status: 0001\nMessage-ID: <real@x>\n\nbody\n\n" + sep + "\nX-Mozilla-Status: 0001\n\nmore body\n"
	dir2 := filepath.Join(t.TempDir(), "imap.c2.example")
	countWrite(t, filepath.Join(dir2, "INBOX"), file2)
	got2, _ := countOnly(t, dir2, false)
	t.Logf("empty-header variant: strict %d loose %d", got2["INBOX"].Messages, got2["INBOX"].Loose)
}

// covers: MA-1279
// R42 shapes that must NOT inflate STRICT (brought in, held): X-Mozilla-Status only in a
// continuation line, only in the body, a header that never ends, CR-only line endings, a separator
// with a time zone or a localised day, X-Mozilla-Status2 only; a separator glued to the line
// before.
func TestR3_R42_ShapesThatMustNotInflate(t *testing.T) {
	sep := "From - Mon Mar  3 09:00:00 2025\n"
	cases := map[string]string{
		"continuation": sep + "Subject: a\n X-Mozilla-Status: 0001\nMessage-ID: <a@x>\n\nb\n",
		"body only":    sep + "Message-ID: <a@x>\n\nX-Mozilla-Status: 0001\n",
		"never ends":   sep + "Message-ID: <a@x>\n" + strings.Repeat("X-J: "+strings.Repeat("q", 100)+"\n", 800) + "X-Mozilla-Status: 0001\n",
		"cr only":      strings.ReplaceAll(sep+"X-Mozilla-Status: 0001\nMessage-ID: <a@x>\n\nb\n", "\n", "\r"),
		"time zone":    "From - Mon Mar  3 09:00:00 2025 +0000\nX-Mozilla-Status: 0001\n\nb\n",
		"localised":    "From - Lun Mar  3 09:00:00 2025\nX-Mozilla-Status: 0001\n\nb\n",
		"status2 only": sep + "X-Mozilla-Status2: 00000000\nMessage-ID: <a@x>\n\nb\n",
	}
	for name, file := range cases {
		dir := filepath.Join(t.TempDir(), "imap.c2.example")
		countWrite(t, filepath.Join(dir, "INBOX"), file)
		got, _ := countOnly(t, dir, false)
		mb := got["INBOX"]
		if mb == nil {
			t.Errorf("%s: no mailbox", name)
			continue
		}
		if mb.Messages != 0 {
			t.Errorf("%s: strict %d (loose %d unsep %d glued %d)", name, mb.Messages, mb.Loose, mb.Unseparated, mb.Glued)
		}
	}
	glued := sep + "X-Mozilla-Status: 0001\n\nbody\nlast line\n" + sep + "X-Mozilla-Status: 0001\n\nb\n"
	dir := filepath.Join(t.TempDir(), "imap.c2.example")
	countWrite(t, filepath.Join(dir, "INBOX"), glued)
	got, _ := countOnly(t, dir, false)
	if mb := got["INBOX"]; mb.Messages != 1 || mb.Glued != 1 {
		t.Errorf("glued: %+v", mb)
	}
}
