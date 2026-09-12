package model

import (
	"testing"
	"time"
)

// covers: MA-233, R1, R3, S39
// The dedup identity and fingerprint fold the IMMUTABLE MIME Date (IdentityDate),
// never the mutable delivery time (Received). The Graph path overwrites Received
// with receivedDateTime for display; if the identity folded Received, a
// no-Message-ID message's sha: identity (and its fingerprint) would DRIFT across a
// pre-go-back → v5 upgrade — duplicating it and phantom-marking the original gone.
// Folding IdentityDate keeps both stable regardless of the Received override, and
// equal to what a pre-go-back message (no override; Received == MIME Date) computed.
func TestIdentityAndFingerprintStableAcrossReceivedOverride(t *testing.T) {
	mimeDate := time.Date(2025, 3, 3, 9, 0, 0, 0, time.UTC)
	legacy := Message{Subject: "s", SenderEmail: "a@x", To: "b@x", PlainBody: "body",
		IdentityDate: mimeDate, Received: mimeDate} // pre-go-back: Received == MIME Date
	upgraded := legacy
	upgraded.Received = time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC) // Graph override differs

	if legacy.Identity() != upgraded.Identity() {
		t.Errorf("no-Message-ID identity drifted with the Received override: %q vs %q", legacy.Identity(), upgraded.Identity())
	}
	if legacy.Fingerprint() != upgraded.Fingerprint() {
		t.Errorf("fingerprint drifted with the Received override: %q vs %q", legacy.Fingerprint(), upgraded.Fingerprint())
	}
}
