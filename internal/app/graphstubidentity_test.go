package app

import (
	"bytes"
	"testing"
	"time"

	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/source"
)

// covers: MA-330, R3, R1
// MS2-AI2. A Graph message with no Message-ID that neither parser can read keeps the
// identity of its received time, as the Graph stub did before the walker was switched on:
// the stub identity folds the listing's receivedDateTime (applyGraphState sets it), so two
// different such messages are two records and not one that hides the other (R1). The
// constant, dateless identity stays for a stub with no received time and for every local
// source (ESC-1 is theirs).
func TestAGraphStubKeepsTheIdentityOfItsReceivedTime(t *testing.T) {
	restore := source.InjectParsePanics(func(d []byte) bool { return bytes.Contains(d, []byte("PANIC-MARKER")) })
	defer restore()
	mk := func(body string) *model.Message {
		return source.ParseRFC822([]byte("From: a@x.example\r\nSubject: s\r\nDate: Mon, 02 Jan 2023 15:04:05 +0000\r\n\r\n" + body + " PANIC-MARKER\r\n"))
	}
	t1 := time.Date(2025, 3, 3, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)

	a, b := mk("first"), mk("second")
	if !parseStub(a) || !parseStub(b) {
		t.Fatalf("the premise: both are stubs, got %q and %q", a.Identity(), b.Identity())
	}
	applyGraphState(a, graph.MessageRef{Received: t1})
	applyGraphState(b, graph.MessageRef{Received: t2})

	if a.Identity() == b.Identity() || a.Fingerprint() == b.Fingerprint() {
		t.Errorf("two stubs received an hour apart share the identity %s and fingerprint %s: the second is skipped as archived", a.Identity(), a.Fingerprint())
	}
	if want := legacyStubIdentity(t1); a.Identity() != want {
		t.Errorf("the stub's identity is %s, want the one it had before the walker, the stub subject at its received time: %s", a.Identity(), want)
	}

	// The twin: no received time from the listing leaves the constant identity.
	c := mk("third")
	applyGraphState(c, graph.MessageRef{})
	if c.Identity() != model.LegacyStubIdentity() {
		t.Errorf("a stub with no received time has the identity %s, want the constant %s", c.Identity(), model.LegacyStubIdentity())
	}
}
