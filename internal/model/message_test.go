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
