package source

// The C2 adversarial pass's fixes in the walk's count (docs/review-thunderbird-completeness-
// c2-adversarial.md, ADV-C2-03, -12, -13, -14 and the identity rule of ADV-C2-02). The attack
// tests are brought in from the pass's clone, each with its positive twin first.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/model"
)

func countOnly(t *testing.T, dir string, distinct bool) (map[string]*MailboxCount, int) {
	t.Helper()
	r, err := openMboxDir(dir, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c := &MailboxCounts{Count: true, Distinct: distinct}
	n := 0
	if err := r.WalkWithFaults(func(folder []string, m *model.Message) error { n++; return nil }, WalkOptions{Faults: func(Fault) {}, Counts: c}); err != nil {
		t.Fatal(err)
	}
	got := map[string]*MailboxCount{}
	for _, mb := range c.Mailboxes {
		got[mb.Rel] = mb
	}
	return got, n
}

// covers: MA-1173
// ADV-C2-12: a maildir folder's count leaves out what is no message of Thunderbird's maildir
// store, a Finder ".DS_Store" and an empty file, which are still handed on (archived) as before.
// Positive twin: three message files count 3.
func TestADV_C2_12_MaildirJunkFilesAreNotCounted(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "imap.c2.example")
	for i := 1; i <= 3; i++ {
		countWrite(t, filepath.Join(dir, "INBOX", "cur", fmt.Sprintf("%d.eml", i)), countMsg(fmt.Sprintf("m%d", i), fmt.Sprintf("m%d@x", i), "0001"))
	}
	got, plain := countOnly(t, dir, false)
	if got["INBOX"] == nil || got["INBOX"].Messages != 3 {
		t.Fatalf("positive twin: %+v", got["INBOX"])
	}
	countWrite(t, filepath.Join(dir, "INBOX", "cur", ".DS_Store"), "\x00\x00\x00\x01Bud1\x00\x00")
	countWrite(t, filepath.Join(dir, "INBOX", "new", ".hidden-note"), countMsg("dot", "dot@x", "0001"))
	countWrite(t, filepath.Join(dir, "INBOX", "cur", "empty"), "")
	got, archived := countOnly(t, dir, false)
	if got["INBOX"].Messages != 3 {
		t.Errorf("a maildir folder with 3 messages, a .DS_Store, a dot-named file and an empty file counts %d (archived %d)", got["INBOX"].Messages, archived)
	}
	if archived <= plain {
		t.Errorf("what the walk hands on changed: %d before the junk files, %d after (the junk is archived as before)", plain, archived)
	}
}

// covers: MA-1173
// ADV-C2-13 (held, brought in): untrusted header shapes never cost more than the 64 KiB header
// bound, a sender's own X-Mozilla-Status after Thunderbird's never changes the count (the first
// field wins), one before it (no Thunderbird line) can only lower X; a 4 MiB header with no blank
// line and 2,000 messages of 60 KiB headers are counted in bounded time. Amended for the identity
// rule: the message whose Message-ID sits past the bound has no key at all (its header block does
// not end within the bound), so it is NoKey, never a per-copy count.
func TestADV_C2_13_HeaderShapesAreBounded(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "imap.c2.example")
	tb := "From: a@x\nX-Mozilla-Status: 0001\nX-Mozilla-Status2: 00000000\nX-Mozilla-Status: 0008\nMessage-ID: <s@x>\nSubject: spoof\n\nbody\n"
	only := "From: a@x\nX-Mozilla-Status: 0008\nMessage-ID: <t@x>\nSubject: sender-only\n\nbody\n"
	countWrite(t, filepath.Join(dir, "INBOX"), countMbox(tb, only))
	got, _ := countOnly(t, dir, true)
	if got["INBOX"].Messages != 1 || got["INBOX"].Expunged != 1 {
		t.Errorf("spoofed status: %+v (want 1 counted, the sender-only one left out: toward behind)", got["INBOX"])
	}
	big := filepath.Join(t.TempDir(), "imap.big.example")
	var b strings.Builder
	// Amended 2026-11-08 (round 2's R2-04): each message begins with X-Mozilla-Status, as
	// Thunderbird writes it, so it is counted for certain.
	b.WriteString("From - Mon Mar  3 09:00:00 2025\nX-Mozilla-Status: 0001\n")
	for b.Len() < 4<<20 {
		b.WriteString("X-Junk: " + strings.Repeat("a", 70) + "\n")
	}
	b.WriteString("Message-ID: <late@x>\n\nbody\n")
	line := "X-H: " + strings.Repeat("b", 1000) + "\n"
	for i := 0; i < 2000; i++ {
		b.WriteString("\nFrom - Mon Mar  3 09:00:00 2025\nX-Mozilla-Status: 0001\n")
		b.WriteString(strings.Repeat(line, 60))
		fmt.Fprintf(&b, "Message-ID: <m%d@x>\n\nbody\n", i)
	}
	countWrite(t, filepath.Join(big, "INBOX"), b.String())
	start := time.Now()
	g, _ := countOnly(t, big, true)
	if d := time.Since(start); d > 60*time.Second {
		t.Errorf("counting took %v", d)
	}
	if g["INBOX"].Messages != 2001 {
		t.Errorf("counted %d, want 2001", g["INBOX"].Messages)
	}
	if g["INBOX"].NoKey != 1 || len(g["INBOX"].Keys) != 2000 {
		t.Errorf("keys %d, no key %d; want 2000 and 1", len(g["INBOX"].Keys), g["INBOX"].NoKey)
	}
}

// covers: MA-1173, MA-1175
// ADV-C2-14: a Message-ID written "Message-ID :" (white space before the colon, RFC 5322's
// obsolete syntax) is read, case-blind and unfolded: two copies of one such message have one key.
// Positive twin: the ordinary spelling.
func TestADV_C2_14_ObsoleteFieldSpellingIsRead(t *testing.T) {
	for _, field := range []string{"Message-ID:", "Message-ID :", "message-id\t:", "MESSAGE-Id  :"} {
		dir := filepath.Join(t.TempDir(), "imap.gmail.com")
		m := "From: a@x\nX-Mozilla-Status: 0001\n" + field + "\n <same@x>\nSubject: s\n\nbody\n"
		countWrite(t, filepath.Join(dir, "INBOX"), countMbox(m))
		countWrite(t, filepath.Join(dir, "[Gmail].sbd", "All Mail"), countMbox(m))
		got, _ := countOnly(t, dir, true)
		in, all := got["INBOX"], got["[Gmail].sbd/All Mail"]
		if len(in.Keys) != 1 || len(all.Keys) != 1 || in.Keys[0] != idKey("same@x") || all.Keys[0] != idKey("same@x") {
			t.Errorf("%q: the Message-ID is not read as same@x in both copies", field)
		}
	}
	// A field that only starts with the name is not it.
	if _, ok := headerValue("Message-IDX: <no@x>\nMessage-ID-Extra : <no@x>", "Message-ID"); ok {
		t.Error("a longer field name was read as Message-ID")
	}
}

// covers: MA-1175
// The C2 pass's identity rule (ADV-C2-02's fix): a message with no Message-ID is known by the
// checksum of its bytes without Thunderbird's per-copy lines (X-Mozilla-Status, -Status2 and
// -Keys of the top header block, with their continuation lines), so the same email in two folders
// has one key even when Thunderbird marked it read in one and not the other; two different
// emails never share one; a header line of the body or a sender's lookalike field is not taken
// out. Positive twin first: one message's key is stable.
func TestAMessageWithNoIDIsKnownByItsContent(t *testing.T) {
	base := "From: a@x\nSubject: s\nX-Mozilla-Status: %s\nX-Mozilla-Status2: %s\nX-Mozilla-Keys: %s\nDate: Mon, 03 Mar 2025 09:00:00 +0000\n\nbody\nX-Mozilla-Status: in the body\n"
	m := func(s, s2, keys string) []byte { return []byte(fmt.Sprintf(base, s, s2, keys)) }
	k1, ok1 := IdentityKey(m("0001", "00000000", "                "))
	k1b, _ := IdentityKey(m("0001", "00000000", "                "))
	if !ok1 || k1 != k1b {
		t.Fatalf("positive twin: one message's key: %v %x %x", ok1, k1, k1b)
	}
	k2, ok2 := IdentityKey(m("0000", "10000000", "$label1\n   continued"))
	if !ok2 || k2 != k1 {
		t.Errorf("the same email with other per-copy lines has another key (%v)", ok2)
	}
	if k, _ := IdentityKey([]byte(strings.Replace(string(m("0001", "00000000", "")), "body\n", "other body\n", 1))); k == k1 {
		t.Error("two different emails share a key")
	}
	if k, _ := IdentityKey([]byte(strings.Replace(string(m("0001", "00000000", "")), "in the body", "changed", 1))); k == k1 {
		t.Error("a body line named like Thunderbird's was taken out")
	}
	if k, _ := IdentityKey([]byte(strings.Replace(string(m("0001", "00000000", "")), "Subject: s", "X-Mozilla-Statuses: s", 1))); k == k1 {
		t.Error("a lookalike field name was taken out")
	}
	// With a Message-ID, the ID decides, whatever else differs.
	idA, _ := IdentityKey([]byte("Message-ID: <x@y>\nSubject: a\n\none\n"))
	idB, _ := IdentityKey([]byte("Message-ID: <x@y>\nSubject: b\n\ntwo\n"))
	if idA != idB || idA != idKey("x@y") {
		t.Error("the Message-ID does not decide")
	}
	// More of Thunderbird's lines than the split's bound: no key.
	if _, ok := IdentityKey([]byte(strings.Repeat("X-Mozilla-Keys: a\n", 9) + "\nbody\n")); ok {
		t.Error("nine of Thunderbird's lines gave a key")
	}
	// A message with no header end within 64 KiB: no key; a short one with headers only has one.
	if _, ok := IdentityKey([]byte("X-A: " + strings.Repeat("a", 70<<10) + "\n")); ok {
		t.Error("a header block with no end within the bound gave a key")
	}
	if _, ok := IdentityKey([]byte("Subject: only headers\n")); !ok {
		t.Error("a short header-only message gave no key")
	}
}

// covers: MA-1173
// ADV-C2-03: the count takes only the pieces that begin at a separator Thunderbird writes (a
// "From " line at the file's start or after an empty line, of Thunderbird's "From - <ctime>"
// shape or the classic "From sender <ctime>"); the reader's other pieces (a body line starting
// "From " that its writer did not escape) are noted as Unseparated and never counted, and the
// walk hands on exactly what it handed on before (what is archived is unchanged). Positive twin:
// the same body escaped counts 5, nothing unseparated.
func TestOnlyThunderbirdsSeparatorsAreCounted(t *testing.T) {
	body := func(prefix string) string {
		var b strings.Builder
		// Amended 2026-11-08 (round 2's R2-04): the message carries X-Mozilla-Status, as every
		// offline copy Thunderbird writes does, so it is counted for certain.
		b.WriteString("From: a@x\nMessage-ID: <crafted@x>\nX-Mozilla-Status: 0001\nSubject: crafted\n\nfirst line\n")
		for i := 0; i < 6; i++ {
			fmt.Fprintf(&b, "%sFrom the desk of Bob, note %d\n", prefix, i)
			if i%2 == 0 {
				b.WriteString("\n") // an empty line, so the next one follows an empty line
			}
		}
		return b.String()
	}
	msgs := func(prefix string) []string {
		var m []string
		for i := 1; i <= 4; i++ {
			m = append(m, countMsg(fmt.Sprintf("in%d", i), fmt.Sprintf("in%d@x", i), "0001"))
		}
		return append(m, body(prefix))
	}
	dir := filepath.Join(t.TempDir(), "imap.c2.example")
	countWrite(t, filepath.Join(dir, "INBOX"), countMbox(msgs(">")...))
	got, plainEscaped := countOnly(t, dir, true)
	if got["INBOX"].Messages != 5 || got["INBOX"].Unseparated != 0 {
		t.Fatalf("positive twin: escaped: %+v", got["INBOX"])
	}
	raw := filepath.Join(t.TempDir(), "imap.c2.example")
	countWrite(t, filepath.Join(raw, "INBOX"), countMbox(msgs("")...))
	plain := walkCounting(t, raw, nil)
	got, n := countOnly(t, raw, true)
	if mb := got["INBOX"]; mb.Messages != 5 || mb.Unseparated != 6 || len(mb.Keys) != 5 {
		t.Errorf("5 messages, one with 6 unescaped From lines: counted %d, unseparated %d, keys %d; want 5, 6, 5", mb.Messages, mb.Unseparated, len(mb.Keys))
	}
	if n != len(plain) || n != 11 || plainEscaped != 5 {
		t.Errorf("what the walk hands on changed: %d counted, %d without counts, want 11 (the archive's own split is unchanged)", n, len(plain))
	}

	// A "From " line of separator shape that does not follow an empty line is no separator: it
	// is glued (amended 2026-11-08 for round 2's R2-05: Glued, never counted, never "behind").
	glued := filepath.Join(t.TempDir(), "s")
	countWrite(t, filepath.Join(glued, "Box"), "From - Mon Mar  3 09:00:00 2025\nFrom: a@x\nMessage-ID: <g@x>\nX-Mozilla-Status: 0001\n\nbody\nFrom - Tue Mar  4 09:00:00 2025\nstill body\n")
	if g, _ := countOnly(t, glued, false); g["Box"].Messages != 1 || g["Box"].Glued != 1 || g["Box"].Unseparated != 0 {
		t.Errorf("a separator-shaped line straight after a body line: %+v", g["Box"])
	}
	// Amended 2026-11-08 (round 2's R2-04, the lead's call: only Thunderbird's own separator).
	// The shapes accepted: Thunderbird's "From - " and ctime, the day space-padded or in two
	// digits (Windows' ctime pads with a zero), CRLF, a first separator after leading empty lines,
	// and the last line with no newline.
	st := "X-Mozilla-Status: 0001\n"
	shapes := filepath.Join(t.TempDir(), "s")
	countWrite(t, filepath.Join(shapes, "Box"), "\r\n\nFrom - Mon Mar  3 09:00:00 2025\r\nX-Mozilla-Status: 0001\r\nSubject: a\r\n\r\nx\r\n\r\n"+
		"From - Mon Mar 03 09:00:00 2025\n"+st+"Subject: b\n\nx\n\n"+
		"From - Sat Nov 17 10:00:00 2026\n"+st+"Subject: c\n\nx\n\n"+
		"From - Sat Nov  7 10:00:00 2026")
	if g, _ := countOnly(t, shapes, false); g["Box"].Messages != 3 || g["Box"].Loose != 1 || g["Box"].Unseparated != 0 || g["Box"].Glued != 0 {
		t.Errorf("the accepted shapes: %+v (want 3 counted and the header-less last one loose)", g["Box"])
	}
	// Shapes refused: no date, a lower-case day, a trailing word, a separator too long (its first
	// 256 bytes alone would pass); and, since round 2, every other sender word (the classic form,
	// a git patch's first line), a time zone, a time without seconds, a trailing space, a
	// three-digit day.
	for _, bad := range []string{"From - x", "From x", "From - mon Mar  3 09:00:00 2025", "From - Mon Mar  3 09:00:00 2025 remote from y",
		"From " + strings.Repeat("a", 300) + " Mon Mar  3 09:00:00 2025", "From - Mon Mar  3 09:00:00 2025" + strings.Repeat(" ", 240) + "x",
		"From alice@example.com Mon Jan 01 00:00:00 2024", "From 3f2a9c0d1e5b7a8c9d0e1f2a3b4c5d6e7f8a9b0c Mon Sep 17 00:00:00 2001",
		"From a@b Sat Nov  7 10:00:00 +0000 2026", "From - Sat Nov  7 10:00:00 +0000 2026", "From - Sat Nov  7 10:00 2026",
		"From - Sat Nov  7 10:00:00 2026 ", "From - Sat Nov 007 10:00:00 2026", "From  - Sat Nov  7 10:00:00 2026"} {
		d := filepath.Join(t.TempDir(), "s")
		countWrite(t, filepath.Join(d, "Box"), "From - Mon Mar  3 09:00:00 2025\n"+st+"Subject: a\n\nx\n\n"+bad+"\n"+st+"Subject: b\n\nx\n")
		if g, _ := countOnly(t, d, false); g["Box"].Messages != 1 || g["Box"].Unseparated != 1 || g["Box"].Loose != 0 {
			t.Errorf("%q taken as a separator: %+v", bad, g["Box"])
		}
	}
}

// covers: MA-1173
// The separator scan follows the reader across read sizes: a stream handed over one byte at a
// time gives the same marks as one read whole, and a "From " that only continues a long line is
// not a line start. Positive twin: the whole read.
func TestTheSeparatorScanIsReadSizeBlind(t *testing.T) {
	data := "From - Mon Mar  3 09:00:00 2025\nA: " + strings.Repeat("z", 5000) + "From - Mon Mar  3 09:00:00 2025\n\nbody\n\nFrom - Tue Mar  4 09:00:00 2025\nB: 1\n\nx\nFrom note\n"
	whole := newSeparatorScan(strings.NewReader(data))
	buf := make([]byte, 1<<16)
	for {
		if _, err := whole.Read(buf); err != nil {
			break
		}
	}
	// Amended 2026-11-08 (the C2 pass round 2): a mark says how the piece begins.
	if fmt.Sprint(whole.marks) != fmt.Sprint([]pieceStart{startSeparator, startSeparator, startBody}) {
		t.Fatalf("positive twin: marks %v", whole.marks)
	}
	bytewise := newSeparatorScan(&oneByte{s: data})
	one := make([]byte, 1)
	for {
		if _, err := bytewise.Read(one); err != nil {
			break
		}
	}
	if fmt.Sprint(bytewise.marks) != fmt.Sprint(whole.marks) {
		t.Errorf("one byte at a time: %v, whole: %v", bytewise.marks, whole.marks)
	}
	if bytewise.start(0) != startBody || bytewise.start(4) != startBody || bytewise.start(2) != startSeparator || bytewise.start(3) != startBody {
		t.Error("start(n) does not follow the marks")
	}
}

type oneByte struct {
	s string
	i int
}

func (o *oneByte) Read(p []byte) (int, error) {
	if o.i >= len(o.s) {
		return 0, fmt.Errorf("EOF: %w", errEOFForTest)
	}
	p[0] = o.s[o.i]
	o.i++
	return 1, nil
}

var errEOFForTest = fmt.Errorf("end")
