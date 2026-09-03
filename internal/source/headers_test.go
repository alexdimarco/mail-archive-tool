package source

import (
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/model"
)

// covers: MA-13, R1, S27
// The MIME parser keeps the headers an inheritor or auditor needs beyond
// From/To/Cc: Bcc, Reply-To, In-Reply-To and References, the Date with its
// ORIGINAL UTC offset (not normalized away), and the raw message bytes so the
// exporter can preserve the original alongside the rendering.
func TestParserKeepsAuditHeadersAndRaw(t *testing.T) {
	raw := "From: Alice <alice@example.com>\r\nTo: bob@example.com\r\nBcc: auditor@example.com\r\n" +
		"Reply-To: board@example.com\r\nIn-Reply-To: <parent@x>\r\nReferences: <root@x> <parent@x>\r\n" +
		"Subject: Thread\r\nMessage-ID: <child@x>\r\nDate: Mon, 01 Jan 2024 09:30:00 -0800\r\n\r\nbody\r\n"
	m := ParseRFC822([]byte(raw))
	if m == nil {
		t.Fatal("nil message")
	}
	if m.Bcc != "auditor@example.com" || m.ReplyTo != "board@example.com" {
		t.Errorf("Bcc/Reply-To not kept: bcc=%q replyto=%q", m.Bcc, m.ReplyTo)
	}
	if m.InReplyTo != "parent@x" || !strings.Contains(m.References, "root@x") {
		t.Errorf("threading headers not kept: in-reply-to=%q references=%q", m.InReplyTo, m.References)
	}
	if _, off := m.Received.Zone(); off != -8*3600 {
		t.Errorf("date offset normalized away: %v", m.Received)
	}
	if string(m.Raw) != raw {
		t.Error("raw bytes not retained")
	}
}

// covers: MA-143, R7, S32
// The PST reader reads PidTagTransportMessageHeaders (0x007D) into
// TransportHeaders so an inheritor/auditor can see the original internet header
// block (Received chain, Return-Path, Authentication-Results, List-Id) that the
// curated header subset drops and that a PST item's absent raw bytes could
// never recover later. The support.pst fixture carries the property on its
// items, so this proves the plumbing end-to-end on a real store.
func TestPSTReaderKeepsTransportHeaders(t *testing.T) {
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	r, err := Open(fixture)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	var total, withHeaders int
	err = r.Walk(func(_ []string, m *model.Message) error {
		total++
		hdr := m.TransportHeaders
		if hdr == "" {
			return nil
		}
		withHeaders++
		// A real header block: its first line is a "Key: value" field, not body.
		firstLine := hdr
		if i := strings.IndexAny(hdr, "\r\n"); i >= 0 {
			firstLine = hdr[:i]
		}
		if !strings.Contains(firstLine, ":") {
			t.Errorf("TransportHeaders does not look like a header block, first line %q", firstLine)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if total == 0 {
		t.Fatal("fixture yielded no messages")
	}
	if withHeaders == 0 {
		t.Fatal("no message carried TransportHeaders — 0x007D was not read")
	}
	t.Logf("transport headers present on %d of %d fixture messages", withHeaders, total)
}

// covers: MA-143, R7, S32
// The raw-bytes sources (mbox/maildir/Evolution, and Graph via ParseRFC822)
// fill TransportHeaders from the header section of Raw — the bytes before the
// first blank line — but only when that blank line falls within the first
// 64 KiB. A body-only blob with no blank line, or one whose headers run past
// the bound, yields "" rather than a misleading partial block.
func TestRawSourcesKeepHeaderBlock(t *testing.T) {
	headers := "Received: from mx.example.com by mail.example.net\r\n" +
		"Authentication-Results: mail.example.net; spf=pass\r\n" +
		"From: Alice <alice@example.com>\r\nSubject: Hi\r\n" +
		"Message-ID: <a@x>\r\nDate: Mon, 01 Jan 2024 09:30:00 -0800"
	raw := headers + "\r\n\r\nthe body text\r\n"
	m := ParseRFC822([]byte(raw))
	if m == nil {
		t.Fatal("nil message")
	}
	if m.TransportHeaders != headers {
		t.Errorf("header block mismatch\n got %q\nwant %q", m.TransportHeaders, headers)
	}

	// A bare-LF blank line terminates the block just the same.
	lf := ParseRFC822([]byte("Subject: LF\nMessage-ID: <b@x>\n\nbody"))
	if lf.TransportHeaders != "Subject: LF\nMessage-ID: <b@x>" {
		t.Errorf("bare-LF header block wrong: %q", lf.TransportHeaders)
	}

	// A body-only blob with no blank line at all: no header block.
	body := ParseRFC822([]byte("just some text with no headers and no blank line"))
	if body.TransportHeaders != "" {
		t.Errorf("body-only blob produced a header block: %q", body.TransportHeaders)
	}

	// The blank line lies beyond 64 KiB: nothing rather than a partial block.
	long := strings.Repeat("X-Filler: pad\r\n", 6000) // ~90 KiB before the blank line
	over := ParseRFC822([]byte(long + "\r\nbody"))
	if over.TransportHeaders != "" {
		t.Errorf("blank line past 64 KiB should yield no block, got %d bytes", len(over.TransportHeaders))
	}
}
