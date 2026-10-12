package model

import (
	"testing"
	"time"
)

// The expected values below were computed by the build before the identity view
// existed (slice S1 of docs/design-mime-fidelity.md), over these two messages with
// no IdentityTerms: a message without terms must keep exactly these.
func identityViewSamples() (a, b *Message, want [2]struct{ id, fp, digest, noAtt string }) {
	d := time.Date(2024, 3, 5, 10, 11, 12, 0, time.FixedZone("x", 3600))
	a = &Message{
		InternetMessageID: "a@x", Subject: "Hello", SenderEmail: "s@x", To: "t@x", Cc: "c@x", Bcc: "b@x",
		IdentityDate: d, HTMLBody: "<p>h</p>", PlainBody: "p", RTFBody: "r",
		Attachments: []Attachment{{Filename: "f.pdf", Size: 10}, {Filename: "g.txt"}},
	}
	b = &Message{
		Subject: "No id", SenderEmail: "s@x", To: "t@x", Received: d,
		PlainBody: "body", Attachments: []Attachment{{Filename: "h.bin", Size: 3}},
	}
	want[0].id, want[0].fp = "mid:a@x", "1e664a862ff7bf78"
	want[0].digest = "c45fb88fa810f401164788d9b5c4df0e47f0e8f7ea163c51d6c0191305b4040f"
	want[0].noAtt = "4ed8b63e746c948b"
	want[1].id = "sha:8e6428bc9b712157c182bf04d3519660a5eba610a78a0c2484e8ea543bf80c4d"
	want[1].fp = "50e5e1682941dc72"
	want[1].digest = "8bceb9ee7633a169d6916dd5f1b041d4b708637bbe861f80a900553e6fa81467"
	want[1].noAtt = "8f31b8b49d278135"
	return a, b, want
}

// termsOf is the identity terms a message's own content fields make: what the v1
// parser would have handed the identity view for it.
func termsOf(m *Message) *IdentityTerms {
	t := m.idv()
	return &t
}

// perturbed is m with every content field a fidelity parser may change moved: the
// subject decoded, the sender found, bodies split, attachments renamed and added.
func perturbed(m *Message) *Message {
	p := *m
	p.Subject, p.SenderEmail, p.To, p.Cc, p.Bcc = "Hellö", "other@x", "z@x", "q@x", "w@x"
	p.InternetMessageID += "-changed"
	p.HTMLBody, p.PlainBody, p.RTFBody = "<p>h</p><p>more</p>", "p\n\nmore", ""
	p.Attachments = []Attachment{{Filename: "renamed.pdf", Size: 11}, {Filename: "inline.png"}, {Filename: "extra.txt"}}
	return &p
}

// covers: MA-330, R3, R2, R27
// The identity view is the only identity input: a message with no IdentityTerms
// keeps the identity, fingerprint, content digest and zero-attachment fingerprint
// it always had, and one whose content a parser has changed keeps them as long as
// it carries the v1 terms.
func TestIdentityReadsTheIdentityViewOnly(t *testing.T) {
	a, b, want := identityViewSamples()
	for i, m := range []*Message{a, b} {
		check := func(label string, m *Message) {
			t.Helper()
			if got := m.Identity(); got != want[i].id {
				t.Errorf("%s: Identity() = %q, want %q", label, got, want[i].id)
			}
			if got := m.Fingerprint(); got != want[i].fp {
				t.Errorf("%s: Fingerprint() = %q, want %q", label, got, want[i].fp)
			}
			if got := m.ContentDigest(); got != want[i].digest {
				t.Errorf("%s: ContentDigest() = %q, want %q", label, got, want[i].digest)
			}
			if got := m.FingerprintNoAttachments(); got != want[i].noAtt {
				t.Errorf("%s: FingerprintNoAttachments() = %q, want %q", label, got, want[i].noAtt)
			}
		}
		check("no terms", m)
		withTerms := *m
		withTerms.IdentityTerms = termsOf(m)
		check("own terms", &withTerms)
		changed := perturbed(m)
		changed.IdentityTerms = termsOf(m)
		check("changed content, v1 terms", changed)
		// The content a parser changed differs from the terms: without them the
		// identity would have moved.
		if i == 0 {
			bare := perturbed(m)
			if bare.Identity() == want[i].id || bare.Fingerprint() == want[i].fp {
				t.Fatal("the perturbed content does not move the identity, so the test proves nothing")
			}
		}
	}
}

// covers: MA-330, R3, R27, MA-233
// The date term is the terms' IdentityDate and, when that is zero, the content's
// Date() exactly as before: a source that overrides Received (Graph) still feeds a
// Date-less identity, and a Date the terms carry is not overridden by it.
func TestIdentityViewKeepsTheDateFallback(t *testing.T) {
	_, b, want := identityViewSamples()
	withTerms := *b
	withTerms.IdentityTerms = termsOf(b)
	if !withTerms.IdentityTerms.IdentityDate.IsZero() {
		t.Fatal("the sample was meant to have no MIME Date")
	}
	if got := withTerms.Identity(); got != want[1].id {
		t.Errorf("Date-less terms: Identity() = %q, want %q (the fallback to Received)", got, want[1].id)
	}
	moved := withTerms
	moved.Received = b.Received.Add(time.Hour)
	if moved.Identity() == want[1].id {
		t.Error("a Received override no longer feeds a Date-less identity")
	}

	dated := *b
	dated.IdentityDate = b.Received.Add(-time.Hour)
	dated.IdentityTerms = termsOf(&dated)
	before := dated.Identity()
	dated.Received = b.Received.Add(24 * time.Hour)
	if dated.Identity() != before {
		t.Error("a Received override moved an identity whose terms carry the MIME Date")
	}
}

// covers: MA-330, R3, R27
// FingerprintNoAttachments drops the terms' attachments too, or a healthy re-read
// of an item whose attachment table failed would not land on its partial record.
func TestFingerprintNoAttachmentsDropsTheTermsAttachments(t *testing.T) {
	a, _, want := identityViewSamples()
	withTerms := *a
	withTerms.IdentityTerms = termsOf(a)
	if got := withTerms.FingerprintNoAttachments(); got != want[0].noAtt {
		t.Errorf("FingerprintNoAttachments() with terms = %q, want %q", got, want[0].noAtt)
	}
	if len(withTerms.IdentityTerms.Attachments) != 2 {
		t.Error("FingerprintNoAttachments edited the message's own terms")
	}
}

// covers: MA-346, R4, R3
// The stem subject is the identity view's: the terms' Subject when set, the content
// Subject otherwise.
func TestIdentitySubjectIsTheViewsSubject(t *testing.T) {
	m := &Message{Subject: "decoded Café"}
	if got := m.IdentitySubject(); got != "decoded Café" {
		t.Errorf("no terms: IdentitySubject() = %q", got)
	}
	m.IdentityTerms = &IdentityTerms{Subject: "raw Caf\xe9"}
	if got := m.IdentitySubject(); got != "raw Caf\xe9" {
		t.Errorf("with terms: IdentitySubject() = %q, want the terms' subject", got)
	}
}

// covers: MA-330, R3, R27
// A Message-ID a source learns out of band feeds the identity whether or not the
// message carries terms.
func TestSetInternetMessageIDFeedsTheIdentity(t *testing.T) {
	plain := &Message{Subject: "(unreadable message)"}
	plain.SetInternetMessageID("listed@x")
	if got := plain.Identity(); got != "mid:listed@x" {
		t.Errorf("no terms: Identity() = %q", got)
	}
	stub := &Message{Subject: "(unreadable message)", IdentityTerms: &IdentityTerms{Subject: "(unreadable message)"}}
	stub.SetInternetMessageID("listed@x")
	if got := stub.Identity(); got != "mid:listed@x" {
		t.Errorf("with terms: Identity() = %q, want mid:listed@x", got)
	}
	if stub.InternetMessageID != "listed@x" {
		t.Error("the content Message-ID was not set")
	}
}

// covers: R1
// An attachment's label is its name, else cid:<Content-ID>, else attachment-<index>.
func TestAttachmentLabel(t *testing.T) {
	for _, c := range []struct {
		a    Attachment
		i    int
		want string
	}{
		{Attachment{Filename: "f.pdf", ContentID: "c1"}, 0, "f.pdf"},
		{Attachment{ContentID: "c1"}, 2, "cid:c1"},
		{Attachment{}, 3, "attachment-3"},
	} {
		if got := c.a.Label(c.i); got != c.want {
			t.Errorf("Label(%d) of %+v = %q, want %q", c.i, c.a, got, c.want)
		}
	}
}
