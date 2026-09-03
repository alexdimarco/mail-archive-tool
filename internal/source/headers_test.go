package source

import (
	"strings"
	"testing"
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
