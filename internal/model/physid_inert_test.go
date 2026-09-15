package model

import "testing"

// covers: MA-239, R1, R3, S39
// PhysID is a capture-time hint, NOT part of the message identity: it must never
// be folded into Identity(), Fingerprint() or contentHash(). Two messages that
// differ ONLY in PhysID must hash identically under all three — otherwise a
// no-Message-ID (sha:) identity would drift across capture (the class MA-233's
// IdentityDate exists to prevent) and a re-observation whose immutable id changed
// would look like a different message.
func TestPhysIDNotInIdentityOrFingerprint(t *testing.T) {
	base := Message{Subject: "s", SenderEmail: "a@x", To: "b@x", PlainBody: "body"}
	withPhys := base
	withPhys.PhysID = "AAAAAA-immutable-id-AAAAAA"
	if base.Identity() != withPhys.Identity() {
		t.Errorf("PhysID leaked into Identity(): %q vs %q", base.Identity(), withPhys.Identity())
	}
	if base.Fingerprint() != withPhys.Fingerprint() {
		t.Errorf("PhysID leaked into Fingerprint(): %q vs %q", base.Fingerprint(), withPhys.Fingerprint())
	}
	if base.contentHash() != withPhys.contentHash() {
		t.Errorf("PhysID leaked into contentHash()")
	}
}
