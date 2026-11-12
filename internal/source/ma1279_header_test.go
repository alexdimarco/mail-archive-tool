package source

// The owner's ruling of 2026-11-08 ("go", the header check; the completeness design §2.4's fold
// "Folded 2026-11-08 [owner ruling: header check]"): a piece of an mbox file that begins at a
// separator Thunderbird writes counts in the STRICT count when its header holds X-Mozilla-Status
// OR when its header is well-formed. Every test goes through the walk on real temp files.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// hdrDate, hdrFrom and hdrID are the three fields the well-formed check looks for.
const (
	hdrDate = "Date: Mon, 03 Mar 2025 09:00:00 +0000\n"
	hdrFrom = "From: Alice <alice@example.com>\n"
	hdrID   = "Message-ID: <w@x>\n"
)

// strictOf walks one INBOX mailbox holding pieces and gives its count.
func strictOf(t *testing.T, pieces ...string) *MailboxCount {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "imap.hdr.example")
	countWrite(t, filepath.Join(dir, "INBOX"), countMbox(pieces...))
	got, archived := countOnly(t, dir, true)
	if archived != len(pieces) {
		t.Fatalf("archived %d of %d pieces", archived, len(pieces))
	}
	return got["INBOX"]
}

// covers: MA-1279
// The ruling's positive cases. Positive twin first: a piece carrying X-Mozilla-Status is strict
// as before, even when the rest of its header is not well-formed (the OR keeps the old path).
// Then, with no X-Mozilla-Status: Date with From, Date with Message-ID, names in any letter case,
// CRLF line endings, a folded (continuation) line, the edge bytes of a field name ("!" and "~"),
// and a piece that is all header (go-mbox drops the empty line before the next separator) are
// each strict.
func TestAWellFormedHeaderCountsStrict(t *testing.T) {
	for name, piece := range map[string]string{
		"status, well-formed":     hdrFrom + hdrDate + hdrID + "X-Mozilla-Status: 0001\n\nbody\n",
		"status, not well-formed": "Subject: no date\nX-Mozilla-Status: 0001\n\nbody\n",
	} {
		if mb := strictOf(t, piece); mb.Messages != 1 || mb.Loose != 0 {
			t.Fatalf("positive twin %s: strict %d loose %d", name, mb.Messages, mb.Loose)
		}
	}
	for name, piece := range map[string]string{
		"date and from":       hdrFrom + "Subject: s\n" + hdrDate + "\nbody\n",
		"date and message-id": hdrID + hdrDate + "\nbody\n",
		"all three":           hdrFrom + hdrDate + hdrID + "\nbody\n",
		"letter case":         "DATE: Mon, 03 Mar 2025 09:00:00 +0000\nfrom: a@x\n\nbody\n",
		"message-id case":     "date: x\nMESSAGE-ID: <c@x>\n\nbody\n",
		"crlf":                strings.ReplaceAll(hdrFrom+hdrDate+hdrID+"\nbody\n", "\n", "\r\n"),
		"continuation":        hdrFrom + "Subject: a long\n subject, folded\n\tand again\n" + hdrDate + "\nbody\n",
		"edge name bytes":     hdrFrom + hdrDate + "!~: edges of ftext\n\nbody\n",
		"empty value":         "Date:\nFrom:\n\nbody\n",
		"all header":          hdrFrom + hdrDate + hdrID,
	} {
		if mb := strictOf(t, piece); mb.Messages != 1 || mb.Loose != 0 {
			t.Errorf("%s: strict %d loose %d, want strict 1", name, mb.Messages, mb.Loose)
		}
	}
}

// covers: MA-1279
// The ruling's negative cases: without X-Mozilla-Status each of these is LENIENT only (Loose), as
// before the ruling. Positive twin first: the same well-formed header is strict.
func TestAHeaderThatIsNotWellFormedStaysLenient(t *testing.T) {
	if mb := strictOf(t, hdrFrom+hdrDate+hdrID+"\nbody\n"); mb.Messages != 1 {
		t.Fatalf("positive twin: strict %d", mb.Messages)
	}
	long := strings.Repeat("X-J: "+strings.Repeat("q", 100)+"\n", 800) // about 84 KB of fields
	for name, piece := range map[string]string{
		"no date":                       hdrFrom + hdrID + "Subject: s\n\nbody\n",
		"no from and no message-id":     hdrDate + "Subject: s\nTo: b@x\n\nbody\n",
		"date only":                     hdrDate + "\nbody\n",
		"a body line in the block":      hdrFrom + hdrDate + "this is a body line\n\nbody\n",
		"a one-word line, no colon":     hdrFrom + hdrDate + "Thanks!\n\nbody\n",
		"continuation first":            " folded first\n" + hdrFrom + hdrDate + "\nbody\n",
		"tab continuation first":        "\tfolded first\n" + hdrFrom + hdrDate + "\nbody\n",
		"empty name":                    hdrFrom + hdrDate + ": no name\n\nbody\n",
		"space in the name":             hdrFrom + hdrDate + "X A: b\n\nbody\n",
		"space-ended date name":         hdrFrom + hdrID + "Date : Mon, 03 Mar 2025\n\nbody\n",
		"DEL in a name":                 hdrFrom + hdrDate + "X\x7fA: b\n\nbody\n",
		"control byte in a name":        hdrFrom + hdrDate + "X\x1fA: b\n\nbody\n",
		"8-bit byte in a name":          hdrFrom + hdrDate + "X\xc3\xa9: b\n\nbody\n",
		"empty header":                  "\n" + hdrFrom + hdrDate + hdrID + "\nbody\n",
		"never ends within 64 KiB":      hdrFrom + hdrDate + hdrID + long + "\nbody\n",
		"never ends, fields after":      long + hdrFrom + hdrDate + hdrID + "\nbody\n",
		"date only in the body":         hdrFrom + hdrID + "\n" + hdrDate + "\nmore\n",
		"from only in a continuation":   hdrDate + "Subject: s\n From: a@x\n\nbody\n",
		"date only in a continuation":   hdrFrom + "Subject: s\n Date: x\n\nbody\n",
		"a field named like date-x":     hdrFrom + "Date-X: y\n\nbody\n",
		"a field named like from-ish":   hdrDate + "Fromage: y\nMessage-IDs: <z@x>\n\nbody\n",
		"x-mozilla-status2 is no proof": "X-Mozilla-Status2: 00000000\nSubject: s\n\nbody\n",
	} {
		if mb := strictOf(t, piece); mb.Messages != 0 || mb.Loose != 1 {
			t.Errorf("%s: strict %d loose %d, want lenient only", name, mb.Messages, mb.Loose)
		}
	}
}

// covers: MA-1279
// The 64 KiB bound, byte for byte (the same bound and the same reading of "ends" as the identity's
// Message-ID, headerWithin): a piece of exactly headerBound bytes that is all header is
// well-formed, one byte more is not; an empty line whose two newlines both lie inside the bound
// ends the block, one byte later it does not. Positive twin first: a short well-formed header.
func TestAWellFormedHeaderEndsWithinTheBound(t *testing.T) {
	if !wellFormedHeader([]byte(hdrFrom + hdrDate + "\nbody\n")) {
		t.Fatal("positive twin: a short well-formed header")
	}
	head := hdrFrom + hdrDate + "X-A: "
	exact := head + strings.Repeat("a", headerBound-len(head))
	if len(exact) != headerBound || !wellFormedHeader([]byte(exact)) {
		t.Errorf("all header, %d bytes: not well-formed", len(exact))
	}
	if wellFormedHeader([]byte(exact + "a")) {
		t.Error("all header, one byte past the bound: well-formed")
	}
	edge := head + strings.Repeat("a", headerBound-len(head)-2) + "\n\nbody\n"
	if !wellFormedHeader([]byte(edge)) {
		t.Error("an empty line ending at the bound: not well-formed")
	}
	past := head + strings.Repeat("a", headerBound-len(head)-1) + "\n\nbody\n"
	if wellFormedHeader([]byte(past)) {
		t.Error("an empty line ending one byte past the bound: well-formed")
	}
}

// covers: MA-1279
// The ruling does not widen what a separator is: a git patch's first line ("From <sha> Mon Sep 17
// 00:00:00 2001") unescaped in an attachment, followed by the patch's own well-formed header, is
// still a body line (Unseparated), never counted. Positive twin: the same line escaped (">From ")
// counts the one real message.
func TestAGitPatchWithAWellFormedHeaderIsStillNoSeparator(t *testing.T) {
	patch := func(prefix string) string {
		return hdrFrom + hdrDate + "Message-ID: <outer@x>\nSubject: [PATCH] fix\nMIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=\"b\"\n\n" +
			"--b\nContent-Type: text/x-patch\nContent-Transfer-Encoding: 7bit\n\n" +
			prefix + "From 3f2a9c0d1e5b7a8c9d0e1f2a3b4c5d6e7f8a9b0c Mon Sep 17 00:00:00 2001\nFrom: Dev <dev@x>\nDate: Mon, 3 Mar 2025 09:00:00 +0000\nMessage-ID: <inner@x>\nSubject: [PATCH] fix\n\n---\n--b--\n"
	}
	if mb := strictOf(t, patch(">")); mb.Messages != 1 || mb.Unseparated != 0 || mb.Loose != 0 {
		t.Fatalf("positive twin: %+v", mb)
	}
	dir := filepath.Join(t.TempDir(), "imap.hdr.example")
	countWrite(t, filepath.Join(dir, "INBOX"), countMbox(patch("")))
	got, archived := countOnly(t, dir, true)
	if mb := got["INBOX"]; mb.Messages != 1 || mb.Unseparated != 1 || mb.Loose != 0 || archived != 2 {
		t.Errorf("an unescaped git patch with a well-formed header: strict %d unseparated %d loose %d archived %d; want 1, 1, 0, 2", mb.Messages, mb.Unseparated, mb.Loose, archived)
	}
}

// covers: MA-1279
// The owner-shaped mailbox under the ruling: 170 messages, 2 of them without X-Mozilla-Status but
// with a well-formed header (as the owner's 2 real ones are taken to be), all count strict: 170,
// none loose. The same 2 with no Date (not well-formed) stay loose, as before: 168 and 2. Positive
// twin: all 170 with the line.
func TestAnOwnerShapedMailboxWithWellFormedHeadersCountsAll(t *testing.T) {
	mbox := func(without int, dated bool) string {
		var b strings.Builder
		for i := 1; i <= 170; i++ {
			status := "0001"
			if i <= without {
				status = ""
			}
			m := countMsg(fmt.Sprintf("m%d", i), fmt.Sprintf("m%d@x", i), status)
			if i <= without && !dated {
				m = strings.Replace(m, "Date: Mon, 03 Mar 2025 09:00:00 +0000\n", "", 1)
			}
			if i%14 == 0 {
				m += "\n>From the archives, a quoted line\n"
			}
			fmt.Fprintf(&b, "From - %s Mar %2d 09:%02d:00 2025\n%s\n", []string{"Mon", "Tue", "Wed"}[i%3], 1+i%28, i%60, m)
		}
		return b.String()
	}
	run := func(without int, dated bool) *MailboxCount {
		dir := filepath.Join(t.TempDir(), "imap.owner.example")
		countWrite(t, filepath.Join(dir, "INBOX"), mbox(without, dated))
		g, _ := countOnly(t, dir, true)
		return g["INBOX"]
	}
	if mb := run(0, true); mb.Messages != 170 || mb.Loose != 0 {
		t.Fatalf("positive twin: %+v", mb)
	}
	if mb := run(2, true); mb.Messages != 170 || mb.Loose != 0 || len(mb.Keys) != 170 || mb.Unseparated+mb.Glued != 0 {
		t.Errorf("2 of 170 without X-Mozilla-Status, well-formed: strict %d loose %d keys %d; want 170, 0, 170", mb.Messages, mb.Loose, len(mb.Keys))
	}
	if mb := run(2, false); mb.Messages != 168 || mb.Loose != 2 || len(mb.LooseKeys) != 2 {
		t.Errorf("2 of 170 without X-Mozilla-Status and without Date: strict %d loose %d; want 168, 2", mb.Messages, mb.Loose)
	}
}
