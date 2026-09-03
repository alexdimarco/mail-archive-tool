package model

import "testing"

// covers: MA-37, R3, R1
// The fallback identity (no Message-ID) must distinguish messages that differ
// only in body — otherwise two distinct drafts collide and one is dropped.
func TestIdentityDistinguishesBodies(t *testing.T) {
	base := func() *Message {
		return &Message{Subject: "Draft", SenderEmail: "me@example.com", To: "you@example.com"}
	}

	a := base()
	a.PlainBody = "let's meet at 3"
	b := base()
	b.PlainBody = "let's meet at 4"
	if a.Identity() == b.Identity() {
		t.Error("messages differing only in body must not share an identity (silent data loss)")
	}

	c := base()
	c.PlainBody = "let's meet at 3"
	if a.Identity() != c.Identity() {
		t.Error("byte-identical messages must share an identity (idempotence)")
	}

	// A real Message-ID always wins over the content hash.
	d := base()
	d.PlainBody = "let's meet at 3"
	d.InternetMessageID = "<abc@example.com>"
	if got := d.Identity(); got != "mid:<abc@example.com>" {
		t.Errorf("Message-ID should determine identity, got %q", got)
	}
}

// covers: MA-145, R3, R1, S32
// TransportHeaders is sender-influenced text kept for display, never part of
// the message's identity: a resend that rewrites only its Received/
// Authentication-Results lines is still one message. So the envelope
// Fingerprint (and the fallback content Identity) must be blind to it —
// otherwise the same mail would be recorded twice, or a fill that re-reads a
// fuller header copy would look like a different message and be dropped.
func TestFingerprintIgnoresTransportHeaders(t *testing.T) {
	base := func() *Message {
		return &Message{
			Subject:     "Quarterly report",
			SenderEmail: "sender@example.com",
			To:          "you@example.com",
			PlainBody:   "see attached",
		}
	}
	a := base()
	a.TransportHeaders = "Received: from mx1\r\nAuthentication-Results: spf=pass"
	b := base()
	b.TransportHeaders = "Received: from mx2 (a totally different chain)\r\nAuthentication-Results: spf=fail"

	if a.Fingerprint() != b.Fingerprint() {
		t.Errorf("Fingerprint changed with TransportHeaders: %q vs %q", a.Fingerprint(), b.Fingerprint())
	}
	// The content-hash identity (used when no Message-ID is present) is also
	// blind to the header block.
	if a.Identity() != b.Identity() {
		t.Errorf("Identity changed with TransportHeaders: %q vs %q", a.Identity(), b.Identity())
	}
}

// covers: MA-145, R3, R1, S35
// Importance, Sensitivity and Unread are mutable capture-time state, NOT part of
// the message's identity: a message later marked read, re-prioritised or
// re-classified is still the same message. So both the envelope Fingerprint and
// the fallback content Identity must be blind to all three — otherwise a re-read
// that saw only a changed flag would look like a different message (double
// record) or a fill would be dropped.
func TestFingerprintIgnoresMessageState(t *testing.T) {
	base := func() *Message {
		return &Message{
			Subject:     "Quarterly report",
			SenderEmail: "sender@example.com",
			To:          "you@example.com",
			PlainBody:   "see attached",
		}
	}
	a := base() // no state at all
	b := base()
	b.Unread = true
	b.Importance = "high"
	b.Sensitivity = "confidential"

	if a.Fingerprint() != b.Fingerprint() {
		t.Errorf("Fingerprint changed with message state: %q vs %q", a.Fingerprint(), b.Fingerprint())
	}
	if a.Identity() != b.Identity() {
		t.Errorf("Identity changed with message state: %q vs %q", a.Identity(), b.Identity())
	}
}
