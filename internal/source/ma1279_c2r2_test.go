package source

// The C2 adversarial pass round 2's fixes in the walk's count
// (docs/review-thunderbird-completeness-c2-adversarial-r2.md: R2-01, R2-03, R2-04, R2-05). The
// round's attack tests are brought in from its clone (.claude/attack-c2-r2), each with its
// positive twin first, beside the builder's guards.

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// r2BigHeader is one email whose header block is near 64 KiB, CRLF throughout (as go-mbox hands a
// piece on). With keys, a per-copy X-Mozilla-Keys line (16 + 80 spaces, Thunderbird's padding)
// sits before the Message-ID; the filler is sized so that, in the copy WITH that line, the
// Message-ID line starts 20 bytes before the 64 KiB bound and so straddles it, while in the copy
// without it the whole header block ends inside the bound.
func r2BigHeader(withKeys bool) string {
	const bound = 64 << 10
	keys := "X-Mozilla-Keys: " + strings.Repeat(" ", 80) + "\r\n"
	head := "X-Mozilla-Status: 0001\r\nX-Mozilla-Status2: 00000000\r\nFrom: a@x\r\nSubject: big\r\n"
	startB := bound - 20
	n := startB - len(keys) - len(head) // filler bytes, the same in both copies
	var f strings.Builder
	line := func(l int) { f.WriteString("X-Junk: " + strings.Repeat("a", l-10) + "\r\n") }
	for n >= 2000 {
		line(1000)
		n -= 1000
	}
	line(n / 2)
	line(n - n/2)
	id := "Message-ID: <" + strings.Repeat("b", 40) + "@x>\r\n"
	h := head
	if withKeys {
		h = "X-Mozilla-Status: 0001\r\nX-Mozilla-Status2: 00000000\r\n" + keys + "From: a@x\r\nSubject: big\r\n"
	}
	return h + f.String() + id + "\r\nbody\r\n"
}

// covers: MA-1280
// R2-01 (brought in): two copies of ONE email that differ only in Thunderbird's per-copy
// X-Mozilla-Keys line have one identity, or the cut one has none; never two. The Message-ID is
// read only from a header block that ends within the 64 KiB bound. Positive twin: the same two
// copies with the same per-copy lines.
func TestR2_01_PerCopyLineShiftsTheIDAcrossTheBound(t *testing.T) {
	a, b := r2BigHeader(false), r2BigHeader(true)
	if i := strings.Index(b, "Message-ID:"); i >= 64<<10 || i+len("Message-ID: <")+1 > 64<<10 || i+len("Message-ID: <")+40 < 64<<10 {
		t.Fatalf("setup: the ID line starts at %d in the copy with keys", i)
	}
	ka, oka := IdentityKey([]byte(a))
	ka2, _ := IdentityKey([]byte(a))
	if !oka || ka != ka2 || ka != idKey(strings.Repeat("b", 40)+"@x") {
		t.Fatalf("positive twin: %v", oka)
	}
	kb, okb := IdentityKey([]byte(b))
	if okb && ka != kb {
		t.Errorf("one email, two copies differing only in X-Mozilla-Keys: two identity keys (the copy whose Message-ID straddles 64 KiB is keyed by a cut ID): Gmail's X counts it twice")
	}
	// Through the walk: INBOX holds the copy with the keys line, All Mail the one without.
	dir := filepath.Join(t.TempDir(), "imap.gmail.com")
	countWrite(t, filepath.Join(dir, "INBOX"), "From - Mon Mar  3 09:00:00 2025\r\n"+b+"\r\n")
	countWrite(t, filepath.Join(dir, "[Gmail].sbd", "All Mail"), "From - Mon Mar  3 09:00:00 2025\r\n"+a+"\r\n")
	got, _ := countOnly(t, dir, true)
	seen := map[[16]byte]bool{}
	for _, mb := range got {
		for _, k := range mb.Keys {
			seen[k] = true
		}
	}
	if len(seen) != 1 || got["INBOX"].NoKey != 1 {
		t.Errorf("walked: %d distinct keys for one email (no key: INBOX %d, All Mail %d); want 1 and INBOX's copy with none", len(seen), got["INBOX"].NoKey, got["[Gmail].sbd/All Mail"].NoKey)
	}
}

// covers: MA-1280
// R2-01, the builder's guards on headerWithin: a header block that ends within the bound, a
// short message that is all header, and one that begins with an empty line (no header: a
// Message-ID in its body is never read) each give their block; a longer message with no empty
// line inside the bound gives none, so its Message-ID is never read, and it has no key at all
// (its content identity has no header end either).
func TestTheMessageIDIsReadOnlyFromAWholeHeaderBlock(t *testing.T) {
	if hb, ok := headerWithin([]byte("Message-ID: <a@x>\n\nbody\n")); !ok || hb != "Message-ID: <a@x>" {
		t.Fatalf("positive twin: %q %v", hb, ok)
	}
	if k, ok := IdentityKey([]byte("Subject: s\nMessage-ID: <short@x>\n")); !ok || k != idKey("short@x") {
		t.Error("a short message that is all header: its Message-ID was not read")
	}
	if hb, ok := headerWithin([]byte("\nMessage-ID: <body@x>\n\nmore\n")); !ok || hb != "" {
		t.Errorf("a message beginning with an empty line: %q %v", hb, ok)
	}
	k1, _ := IdentityKey([]byte("\r\nMessage-ID: <body@x>\r\n\r\none\r\n"))
	if k1 == idKey("body@x") {
		t.Error("a Message-ID in the body of a message with no header was read")
	}
	long := []byte("X-A: " + strings.Repeat("a", 70<<10) + "\nMessage-ID: <late@x>\n\nbody\n")
	if _, ok := headerWithin(long); ok {
		t.Error("a header block that ends past the bound was taken")
	}
	if _, ok := IdentityKey(long); ok {
		t.Error("a message whose header ends past the bound got a key")
	}
	// Exactly at the bound: a message of headerBound bytes with no empty line is all header.
	exact := []byte("Message-ID: <edge@x>\nX-A: " + strings.Repeat("a", headerBound-len("Message-ID: <edge@x>\nX-A: ")))
	if k, ok := IdentityKey(exact); len(exact) != headerBound || !ok || k != idKey("edge@x") {
		t.Errorf("a %d-byte all-header message: %v", len(exact), ok)
	}
	if _, ok := IdentityKey(append(exact, 'a')); ok {
		t.Error("one byte past the bound, all header: a key")
	}
	// Through the walk: a loose piece (no X-Mozilla-Status) whose header runs past the bound is
	// loose with no key; one with the line, counted with no key.
	dir := filepath.Join(t.TempDir(), "imap.gmail.com")
	countWrite(t, filepath.Join(dir, "INBOX"), countMbox(string(long), "X-Mozilla-Status: 0001\n"+string(long)))
	g, _ := countOnly(t, dir, true)
	if mb := g["INBOX"]; mb.Loose != 1 || mb.LooseNoKey != 1 || len(mb.LooseKeys) != 0 || mb.Messages != 1 || mb.NoKey != 1 {
		t.Errorf("two pieces with no header end within the bound: %+v", mb)
	}
}

// covers: MA-1175
// R2-02 (held, brought in): the identity's reading of the Message-ID. Folded and unfolded, angle
// brackets and white space inside them, the obsolete spelling: one key. Two Message-ID headers:
// the first wins. A Message-ID only in the body is never read (two ID-less emails with the same
// body line keep two keys). An encoded word is hashed as written.
func TestR2_02_MessageIDShapes(t *testing.T) {
	msg := func(idHeaders, body string) []byte {
		return []byte("From: a@x\nSubject: s\n" + idHeaders + "X-Mozilla-Status: 0001\n\n" + body + "\n")
	}
	key := func(b []byte) [16]byte {
		k, ok := IdentityKey(b)
		if !ok {
			t.Fatalf("no key for %q", b)
		}
		return k
	}
	base := key(msg("Message-ID: <abc@x>\n", "one"))
	for _, h := range []string{"Message-ID:\n <abc@x>\n", "message-id:   <abc@x>  \n", "MESSAGE-ID : <abc@x>\n", "Message-ID:\t< abc@x >\n", "Message-ID: abc@x\n", "Message-ID:\r\n\t<abc@x>\r\n"} {
		if k := key(msg(h, "other body")); k != base {
			t.Errorf("%q: another key than <abc@x>", h)
		}
	}
	if key(msg("Message-ID: <abc@x>\nMessage-ID: <zzz@x>\n", "b")) != base {
		t.Error("two Message-ID headers: the first does not win")
	}
	b1, b2 := key(msg("", "Message-ID: <abc@x>\nfirst")), key(msg("", "Message-ID: <abc@x>\nsecond"))
	if b1 == b2 || b1 == base {
		t.Error("a Message-ID in the body was read as the identity")
	}
	enc := key(msg("Message-ID: =?utf-8?q?abc=40x?=\n", "b"))
	if enc != key(msg("Message-ID: =?utf-8?q?abc=40x?=\n", "c")) {
		t.Error("an encoded-word Message-ID is not stable")
	}
}

// covers: MA-1280
// R2-03 (brought in): CRLF and LF copies of one ID-less email have one key, through an mbox walk
// (go-mbox writes CRLF) and as bytes on disk (a maildir file is hashed as it is). For counting
// only: the gmail-dedup design keeps its stricter, byte-exact identity. Positive twin: the mbox
// walk.
func TestR2_03_LineEndingsOfOneEmail(t *testing.T) {
	lf := "From: a@x\nSubject: no id\nX-Mozilla-Status: 0001\nX-Mozilla-Status2: 00000000\n\nbody\n"
	crlf := strings.ReplaceAll(lf, "\n", "\r\n")
	dir := filepath.Join(t.TempDir(), "imap.gmail.com")
	countWrite(t, filepath.Join(dir, "INBOX"), "From - Mon Mar  3 09:00:00 2025\n"+lf+"\n")
	countWrite(t, filepath.Join(dir, "[Gmail].sbd", "All Mail"), "From - Mon Mar  3 09:00:00 2025\r\n"+crlf+"\r\n")
	got, _ := countOnly(t, dir, true)
	seen := map[[16]byte]bool{}
	for _, mb := range got {
		for _, k := range mb.Keys {
			seen[k] = true
		}
	}
	if len(seen) != 1 {
		t.Fatalf("positive twin: mbox: LF and CRLF copies of one email: %d keys", len(seen))
	}
	k1, _ := IdentityKey([]byte(lf))
	k2, _ := IdentityKey([]byte(crlf))
	if k1 != k2 {
		t.Errorf("maildir (bytes as on disk): LF and CRLF copies of one ID-less email have two keys")
	}
	// Through a maildir walk too.
	md := filepath.Join(t.TempDir(), "imap.gmail.com")
	countWrite(t, filepath.Join(md, "INBOX", "cur", "1"), lf)
	countWrite(t, filepath.Join(md, "All Mail", "cur", "1"), crlf)
	g, _ := countOnly(t, md, true)
	if g["INBOX"] == nil || g["All Mail"] == nil || len(g["INBOX"].Keys) != 1 || g["INBOX"].Keys[0] != g["All Mail"].Keys[0] {
		t.Errorf("a maildir walk: LF and CRLF copies of one email are not one key: %+v %+v", g["INBOX"], g["All Mail"])
	}
	// Two different emails stay two, and a lone CR is content.
	k3, _ := IdentityKey([]byte(strings.Replace(lf, "body", "bo\rdy", 1)))
	if k3 == k1 {
		t.Error("a lone CR inside a line was taken as a line ending")
	}
	k4, _ := IdentityKey([]byte(strings.TrimSuffix(lf, "\n") + "\r"))
	k5, _ := IdentityKey([]byte(strings.TrimSuffix(lf, "\n")))
	if k4 == k5 {
		t.Error("a CR at the very end of an email was dropped")
	}
}

// covers: MA-1280
// The line-ending writer reads CRLF as LF across write boundaries and keeps every other CR:
// "\r\r\n" is "\r\n", a CR at the very end is kept, and the bytes are the same whatever the
// writes' sizes. Positive twin: a whole write.
func TestTheLineEndingWriterIsWriteSizeBlind(t *testing.T) {
	in := "a\r\nb\r\r\nc\rd\r\n\r\ne\r"
	want := "a\nb\r\nc\rd\n\ne\r"
	var whole bytes.Buffer
	w := &lfWriter{w: &whole}
	w.Write([]byte(in))
	w.flush()
	if whole.String() != want {
		t.Fatalf("positive twin: %q, want %q", whole.String(), want)
	}
	for size := 1; size <= 3; size++ {
		var b bytes.Buffer
		w := &lfWriter{w: &b}
		for i := 0; i < len(in); i += size {
			w.Write([]byte(in[i:min(len(in), i+size)]))
		}
		w.flush()
		if b.String() != want {
			t.Errorf("writes of %d: %q, want %q", size, b.String(), want)
		}
	}
}

// covers: MA-1279
// R2-04 (brought in): a body line of a separator's shape after an empty line. The realistic case
// is a git format-patch file sent as a 7bit text attachment: its first line, "From <sha> Mon Sep
// 17 00:00:00 2001", follows the part's empty line. Only Thunderbird's own "From - " separator
// counts, so it is a body line (Unseparated). Thunderbird's own separator quoted in a body after
// an empty line starts a piece with no X-Mozilla-Status: loose, never counted for certain.
// Positive twin: the same line escaped (">From ", as current Thunderbird writes) counts one.
func TestR2_04_SeparatorShapedBodyLine(t *testing.T) {
	patch := func(prefix string) string {
		return "From: Dev <dev@x>\nTo: list@x\nSubject: [PATCH] fix\nMessage-ID: <p1@x>\nX-Mozilla-Status: 0001\nX-Mozilla-Status2: 00000000\n" +
			"MIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=\"b\"\n\n--b\nContent-Type: text/plain\n\nsee the patch\n\n--b\n" +
			"Content-Type: text/x-patch; name=\"0001-fix.patch\"\nContent-Transfer-Encoding: 7bit\nContent-Disposition: attachment; filename=\"0001-fix.patch\"\n\n" +
			prefix + "From 3f2a9c0d1e5b7a8c9d0e1f2a3b4c5d6e7f8a9b0c Mon Sep 17 00:00:00 2001\nFrom: Dev <dev@x>\nDate: Mon, 3 Mar 2025 09:00:00 +0000\nSubject: [PATCH] fix\n\n---\n a | 1 +\n--b--\n"
	}
	for _, c := range []struct {
		name, prefix  string
		want, unsplit int64
	}{{"escaped (positive twin)", ">", 1, 0}, {"unescaped git patch", "", 1, 1}} {
		dir := filepath.Join(t.TempDir(), "imap.c2.example")
		countWrite(t, filepath.Join(dir, "INBOX"), countMbox(patch(c.prefix)))
		got, archived := countOnly(t, dir, true)
		mb := got["INBOX"]
		if mb.Messages != c.want || mb.Loose != 0 || mb.Unseparated != c.unsplit {
			t.Errorf("%s: counted %d (loose %d, unseparated %d, archived %d), want %d", c.name, mb.Messages, mb.Loose, mb.Unseparated, archived, c.want)
		}
	}
	// Thunderbird's own separator text quoted in a plain-text body after an empty line.
	quoted := countMsg("quoting an mbox", "q1@x", "0001") + "\nFrom - Mon Mar  3 09:00:00 2025\nFrom: someone@x\n"
	dir := filepath.Join(t.TempDir(), "imap.c2.example")
	countWrite(t, filepath.Join(dir, "INBOX"), countMbox(quoted))
	got, _ := countOnly(t, dir, true)
	if got["INBOX"].Messages != 1 || got["INBOX"].Loose != 1 || len(got["INBOX"].Keys) != 1 || len(got["INBOX"].LooseKeys) != 1 {
		t.Errorf("Thunderbird's separator quoted in a body after an empty line: %+v, want 1 counted and 1 loose", got["INBOX"])
	}
}

// covers: MA-1279
// R2-05 (held, brought in; amended for the fix): a real separator with no empty line before it
// (a writer that does not end a message with one) is glued: never counted, so X is lower, and
// its mailbox is told apart (Glued) so the verdict never calls it behind. Positive twin: the
// same five with empty lines count five.
func TestR2_05_SeparatorWithNoEmptyLineBefore(t *testing.T) {
	var with, without strings.Builder
	for i := 1; i <= 5; i++ {
		m := countMsg(fmt.Sprintf("m%d", i), fmt.Sprintf("m%d@x", i), "0001") // ends "body\n", no empty line
		with.WriteString("From - Mon Mar  3 09:00:00 2025\n" + m + "\n")
		without.WriteString("From - Mon Mar  3 09:00:00 2025\n" + m)
	}
	dir := filepath.Join(t.TempDir(), "imap.c2.example")
	countWrite(t, filepath.Join(dir, "INBOX"), with.String())
	if got, _ := countOnly(t, dir, true); got["INBOX"].Messages != 5 || got["INBOX"].Glued != 0 {
		t.Fatalf("positive twin: %+v", got["INBOX"])
	}
	dir = filepath.Join(t.TempDir(), "imap.c2.example")
	countWrite(t, filepath.Join(dir, "INBOX"), without.String())
	got, archived := countOnly(t, dir, true)
	if mb := got["INBOX"]; mb.Messages != 1 || mb.Glued != 4 || mb.Unseparated != 0 || archived != 5 {
		t.Errorf("5 messages, no empty line before 4 separators: counted %d, glued %d, unseparated %d, archived %d; want 1, 4, 0, 5", mb.Messages, mb.Glued, mb.Unseparated, archived)
	}
}

// covers: MA-1279
// The owner's real profile, as counts (the lead's count-only scan with the owner's permission,
// 2026-11-08): every "From " line is "From - <ctime>" after an empty line or at the file's start,
// body lines are escaped ">From ", and 2 of 318,856 messages carry no X-Mozilla-Status. An
// owner-shaped mailbox of 170 such messages, 2 without the line and a dozen escaped body lines,
// counts 168 for certain and 2 loose, nothing split. Amended 2026-11-08 by the owner's ruling (the
// header check): the 2 here also have no Date line, so their header is not well-formed and they
// stay loose; with a well-formed header they count strict
// (TestAnOwnerShapedMailboxWithWellFormedHeadersCountsAll). Positive twin: all 170 with the line.
func TestAnOwnerShapedMailboxCountsStrictAndLenient(t *testing.T) {
	mbox := func(without int) string {
		var b strings.Builder
		for i := 1; i <= 170; i++ {
			status := "0001"
			if i <= without {
				status = ""
			}
			m := countMsg(fmt.Sprintf("m%d", i), fmt.Sprintf("m%d@x", i), status)
			if i <= without {
				m = strings.Replace(m, "Date: Mon, 03 Mar 2025 09:00:00 +0000\n", "", 1)
			}
			if i%14 == 0 {
				m += "\n>From the archives, a quoted line\n"
			}
			fmt.Fprintf(&b, "From - %s Mar %2d 09:%02d:00 2025\n%s\n", []string{"Mon", "Tue", "Wed"}[i%3], 1+i%28, i%60, m)
		}
		return b.String()
	}
	dir := filepath.Join(t.TempDir(), "imap.owner.example")
	countWrite(t, filepath.Join(dir, "INBOX"), mbox(0))
	if g, _ := countOnly(t, dir, false); g["INBOX"].Messages != 170 || g["INBOX"].Loose != 0 || g["INBOX"].Unseparated+g["INBOX"].Glued != 0 {
		t.Fatalf("positive twin: %+v", g["INBOX"])
	}
	dir = filepath.Join(t.TempDir(), "imap.owner.example")
	countWrite(t, filepath.Join(dir, "INBOX"), mbox(2))
	if g, _ := countOnly(t, dir, false); g["INBOX"].Messages != 168 || g["INBOX"].Loose != 2 || g["INBOX"].Unseparated+g["INBOX"].Glued != 0 {
		t.Errorf("2 of 170 without X-Mozilla-Status: %+v, want 168 counted, 2 loose, nothing split", g["INBOX"])
	}
}
