package export

import (
	"io"
	"testing"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// covers: MA-244, R1, R3, S38, S39
// The #8 closure core (HC1, rev-6.1 content-hash arbiter): two DISTINCT messages
// that reuse one Message-ID with an IDENTICAL envelope — so their fingerprints
// COLLIDE — but different bodies and different immutable ids are told apart by the
// recorded body-inclusive CONTENT HASH and BOTH survive. The second is filed under
// a key qualified by a HASH OF THE IMMUTABLE ID, never the fingerprint (an
// fp-qualified key would collide and overwrite the first — an R1 drop). This is the
// residual the floor could only bound-and-log ($100 vs $250 invoice); with immutable
// ids and the content hash it is closed — and it needs NO preserved .eml (-raw).
func TestDistinctPhysIDSplitSameEnvelope(t *testing.T) {
	out := t.TempDir()
	m := mustManifest(t)
	e := mbwExporter(out, m)

	mk := func(body, phys string) *model.Message {
		return &model.Message{Subject: "Invoice", Received: testDate, InternetMessageID: "<dup@x>",
			SenderEmail: "billing@acme.com", To: "bob@corp.com", PlainBody: body, PhysID: phys}
	}
	a := mk("You owe $100", "IMM-A")
	b := mk("You owe $250", "IMM-B")
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatalf("premise broken: fingerprints differ (%s vs %s) — need an identical envelope", a.Fingerprint(), b.Fingerprint())
	}
	if a.ContentDigest() == b.ContentDigest() {
		t.Fatalf("premise broken: content hashes equal — need distinct bodies to split")
	}
	for _, msg := range []*model.Message{a, b} {
		if _, err := e.Export("store", []string{"Inbox"}, msg); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(m.KeysForIdentity("mid:<dup@x>")); n != 2 {
		t.Fatalf("identity has %d records, want 2 (both distinct reuses kept — #8 closed)", n)
	}
	if e.Stats.Exported != 2 || countSuffix(t, out, ".html") != 2 {
		t.Errorf("exported=%d html=%d, want 2/2 (no drop, no overwrite)", e.Stats.Exported, countSuffix(t, out, ".html"))
	}
	base := state.LiveKey("store", "mid:<dup@x>")
	qk := state.Qualify(base, util.HashHex("IMM-B", 8))
	rb, ok := m.Get(qk)
	if !ok {
		t.Fatalf("no record at the PhysID-hash-qualified key (a fp-qualified split would have collided and dropped one)")
	}
	ra, _ := m.Get(base)
	if ra.PhysID != "IMM-A" || rb.PhysID != "IMM-B" {
		t.Errorf("stored PhysIDs = %q / %q, want IMM-A / IMM-B", ra.PhysID, rb.PhysID)
	}
}

// covers: MA-245, R1, R17, S38
// phys-churn (HC7, rev-6.1 content-hash arbiter): the SAME message re-observed with
// a REISSUED immutable id (a restore / cross-tenant migration) is recognized by its
// identical CONTENT HASH, adopted in place (ONE record, no split/duplicate), its
// stored PhysID updated to the new id, and a phys-churn anomaly logged. The content
// hash proves it, so a distinct reuse (MA-244) is never swept up with a churn — and
// it works with NO preserved .eml. The content hash excludes transport headers, so a
// migration that rewrites Received/etc. is still recognized as the same message.
func TestPhysChurnAdoptsInPlace(t *testing.T) {
	out := t.TempDir()
	m := mustManifest(t)
	e := mbwExporter(out, m)

	mk := func(phys string) *model.Message {
		return &model.Message{Subject: "Invoice", Received: testDate, InternetMessageID: "<inv@x>",
			SenderEmail: "billing@acme.com", To: "bob@corp.com", PlainBody: "You owe $100", PhysID: phys}
	}
	if _, err := e.Export("store", []string{"Inbox"}, mk("IMM-OLD")); err != nil {
		t.Fatal(err)
	}
	base := state.LiveKey("store", "mid:<inv@x>")
	if r, _ := m.Get(base); r.PhysID != "IMM-OLD" || r.ContentHash == "" {
		t.Fatalf("first export: PhysID=%q ContentHash=%q, want IMM-OLD + a stored hash", r.PhysID, r.ContentHash)
	}

	// A migration reissued the id; the content is unchanged (same content hash).
	if _, err := e.Export("store", []string{"Inbox"}, mk("IMM-NEW")); err != nil {
		t.Fatal(err)
	}
	if n := len(m.KeysForIdentity("mid:<inv@x>")); n != 1 {
		t.Fatalf("identity has %d records after a churn, want 1 (adopted in place, not duplicated)", n)
	}
	if r, _ := m.Get(base); r.PhysID != "IMM-NEW" {
		t.Errorf("stored PhysID = %q, want IMM-NEW (updated to the reissued id)", r.PhysID)
	}
	if countSuffix(t, out, ".html") != 1 {
		t.Errorf("html files = %d, want 1 (no duplicate written)", countSuffix(t, out, ".html"))
	}
	churn := false
	for _, is := range e.Issues {
		if is.Kind == "phys-churn" && is.Detail == "mid:<inv@x>" {
			churn = true
		}
	}
	if !churn {
		t.Errorf("no phys-churn anomaly logged for the reissued id (Issues=%v)", e.Issues)
	}
}

// covers: MA-248, R1, R3, S38
// The content-hash arbiter must be at least as discriminating as Fingerprint
// (adversarial re-check 2026-09-16): two DISTINCT reuses of one Message-ID that are
// identical in subject/sender/To/date/body and share the same attachment COUNT but
// differ only in an attachment NAME (report-jan.pdf vs report-feb.pdf) — or only in
// Cc — must get DIFFERENT content digests and BOTH survive. A coarse digest (folding
// only the attachment count, not names) collides them and the closure adopts+drops
// one (a silent R1 loss the fingerprint split would otherwise prevent).
func TestContentDigestDistinguishesAttachmentAndCc(t *testing.T) {
	att := func(name string) model.Attachment {
		return model.Attachment{Filename: name, Size: 4, WriteTo: func(w io.Writer) (int64, error) { n, e := w.Write([]byte("data")); return int64(n), e }}
	}
	mk := func(attName, cc, phys string) *model.Message {
		return &model.Message{Subject: "Daily Report", Received: testDate, InternetMessageID: "<rep@x>",
			SenderEmail: "reports@acme.com", To: "bob@corp.com", Cc: cc, PlainBody: "See attached", PhysID: phys,
			Attachments: []model.Attachment{att(attName)}}
	}
	// Unit level: the digest separates an attachment-name-only and a Cc-only diff.
	if mk("report-jan.pdf", "", "A").ContentDigest() == mk("report-feb.pdf", "", "B").ContentDigest() {
		t.Errorf("ContentDigest collides on an attachment-name-only difference — a distinct reuse would be dropped (R1)")
	}
	if mk("report-jan.pdf", "", "A").ContentDigest() == mk("report-jan.pdf", "carol@corp.com", "B").ContentDigest() {
		t.Errorf("ContentDigest collides on a Cc-only difference — a distinct reuse would be dropped (R1)")
	}

	// Exporter level: two attachment-only-differing reuses BOTH survive (split).
	out := t.TempDir()
	m := mustManifest(t)
	e := mbwExporter(out, m)
	for _, msg := range []*model.Message{mk("report-jan.pdf", "", "IMM-A"), mk("report-feb.pdf", "", "IMM-B")} {
		if _, err := e.Export("store", []string{"Inbox"}, msg); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(m.KeysForIdentity("mid:<rep@x>")); n != 2 {
		t.Fatalf("identity has %d records, want 2 (attachment-only-differing reuses must both survive — R1)", n)
	}
}
