package export

import (
	"testing"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: MA-223, R1, R3, S39
// Adopt-never-split (design rev-4 §3): when a DOWNLOAD lands on a same-identity
// sibling whose stored fingerprint is LEGACY-scheme (FpScheme==0 — a pre-v5 or
// empty fp, NOT comparable to a current fingerprint because the go-back work
// changed the fingerprint's date term), the exporter treats it as the SAME
// message and ADOPTS the record — updates it in place with the current-scheme
// fingerprint — rather than #fp-splitting it into a second copy. Without this a
// migrated record would be re-split into a duplicate (R3) on its first download
// (a no-Message-ID re-observation, a fillable retry, or — later — an ImmutableId-
// forced fetch), the exact re-duplication the option-D floor exists to avoid.
// A genuine CURRENT-scheme mismatch still splits (R1) — proven by
// TestMailboxWideIDReuseSplit.
func TestAdoptNeverSplitLegacyScheme(t *testing.T) {
	out := t.TempDir()
	m := mustManifest(t)
	e := mbwExporter(out, m)
	e.RunAt = testDate

	msg := &model.Message{Subject: "Legacy note", Received: testDate, SenderEmail: "a@ex.com",
		InternetMessageID: "<leg@x>", PlainBody: "the body"}
	base := state.LiveKey("store", msg.Identity())

	// Pre-existing LEGACY-scheme record for this identity, with a DIFFERENT stored
	// fingerprint (as a migrated v3 record has — its legacy fp is not comparable).
	m.Add(base, state.Record{
		Path: "store/Inbox/leg.html", Folder: "Inbox", FirstFolder: "Inbox", Present: true,
		Fingerprint: "0000000000000000", FpScheme: 0,
	})

	wrote, err := e.Export("store", []string{"Inbox"}, msg)
	if err != nil {
		t.Fatal(err)
	}
	// Adopt = the seen, complete legacy record is SKIPPED (no rewrite), NOT
	// #fp-split into a second copy. So nothing is written and exactly ONE record
	// remains for the identity, still at the base live key.
	if wrote {
		t.Errorf("a complete legacy-scheme record was re-written (want a skip — adopt, not re-export)")
	}
	if refs := m.KeysForIdentity(msg.Identity()); len(refs) != 1 {
		t.Errorf("identity has %d records after adopt, want 1 (a legacy-scheme sibling was #fp-split into a duplicate — R3)", len(refs))
	}
	if _, ok := m.Get(base); !ok {
		t.Errorf("the base live key %q was split away (a legacy sibling must be adopted, not qualified)", base)
	}
	// A #fp-qualified sibling must NOT have been created.
	if _, ok := m.Get(state.Qualify(base, msg.Fingerprint())); ok {
		t.Errorf("adopt-never-split failed: a #fp-qualified duplicate of the migrated record was created")
	}
}
