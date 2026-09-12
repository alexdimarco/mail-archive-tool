package export

import (
	"testing"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// mbwExporter is an incremental exporter with the live-path mailbox-wide dedup
// flag set (as RunGraph sets it).
func mbwExporter(out string, m *state.Manifest) *Exporter {
	e := incExporter(out, m)
	e.DedupMailboxWide = true
	return e
}

// covers: MA-204, R3, R2
// The Exporter.DedupMailboxWide flag selects the key. OFF (a one-shot local
// import) keeps the folder-scoped key, so the same mail filed in two folders is
// exported to BOTH (R3). ON keys the message mailbox-wide by identity, so the
// same mail seen in two folders is stored ONCE — the second observation is a
// skip, not a second copy.
func TestDedupMailboxWideKey(t *testing.T) {
	sameEnvelope := func() *model.Message {
		return &model.Message{Subject: "Quarterly", Received: testDate, InternetMessageID: "<same@x>",
			SenderEmail: "a@ex.com", To: "b@ex.com", PlainBody: "body"}
	}

	// Folder-scoped (flag off): two folders → two files, two records (R3).
	outOff := t.TempDir()
	mOff := mustManifest(t)
	eOff := incExporter(outOff, mOff)
	for _, folder := range [][]string{{"Inbox"}, {"Archive"}} {
		if _, err := eOff.Export("store", folder, sameEnvelope()); err != nil {
			t.Fatal(err)
		}
	}
	if eOff.Stats.Exported != 2 || mOff.Len() != 2 || countSuffix(t, outOff, ".html") != 2 {
		t.Fatalf("flag OFF: exported=%d manifest=%d html=%d, want 2/2/2 (R3: one copy per folder)",
			eOff.Stats.Exported, mOff.Len(), countSuffix(t, outOff, ".html"))
	}

	// Mailbox-wide (flag on): the same mail in two folders → ONE file, ONE record.
	outOn := t.TempDir()
	mOn := mustManifest(t)
	eOn := mbwExporter(outOn, mOn)
	for _, folder := range [][]string{{"Inbox"}, {"Archive"}} {
		if _, err := eOn.Export("store", folder, sameEnvelope()); err != nil {
			t.Fatal(err)
		}
	}
	if eOn.Stats.Exported != 1 || eOn.Stats.SkippedManifest != 1 || mOn.Len() != 1 || countSuffix(t, outOn, ".html") != 1 {
		t.Fatalf("flag ON: exported=%d skipped=%d manifest=%d html=%d, want 1/1/1/1 (one copy per mailbox)",
			eOn.Stats.Exported, eOn.Stats.SkippedManifest, mOn.Len(), countSuffix(t, outOn, ".html"))
	}
	// The one record sits at the mailbox-wide (folder-less) key and carries its
	// first-captured folder.
	base := state.LiveKey("store", "mid:<same@x>")
	rec, ok := mOn.Get(base)
	if !ok {
		t.Fatalf("no record at the mailbox-wide key %q", base)
	}
	if rec.FirstFolder != "Inbox" {
		t.Errorf("FirstFolder = %q, want Inbox (first-captured)", rec.FirstFolder)
	}
}

// covers: MA-203, R1, R3
// With the mailbox-wide flag, two DIFFERENT messages that reuse one Message-ID in
// DIFFERENT folders both survive: the second (different envelope signature) is
// filed under a #fp-qualified mailbox-wide key, never mistaken for a duplicate of
// the first (R1). This is MA-86's within-folder split, holding mailbox-wide.
func TestMailboxWideIDReuseSplit(t *testing.T) {
	out := t.TempDir()
	m := mustManifest(t)
	e := mbwExporter(out, m)

	a := &model.Message{Subject: "First", Received: testDate, InternetMessageID: "<dup@x>", SenderEmail: "a@ex.com", PlainBody: "alpha"}
	b := &model.Message{Subject: "Second", Received: testDate, InternetMessageID: "<dup@x>", SenderEmail: "a@ex.com", PlainBody: "beta"}

	wroteA, err := e.Export("store", []string{"Inbox"}, a)
	if err != nil || !wroteA {
		t.Fatalf("A: wrote=%v err=%v", wroteA, err)
	}
	wroteB, err := e.Export("store", []string{"Sent"}, b)
	if err != nil {
		t.Fatal(err)
	}
	if !wroteB {
		t.Fatal("B (a distinct message reusing the id in another folder) was skipped — a distinct message was silently dropped (R1)")
	}
	if e.Stats.Exported != 2 || m.Len() != 2 || countSuffix(t, out, ".html") != 2 {
		t.Fatalf("distinct id-reuse: exported=%d manifest=%d html=%d, want 2/2/2 (both survive mailbox-wide)",
			e.Stats.Exported, m.Len(), countSuffix(t, out, ".html"))
	}
	base := state.LiveKey("store", "mid:<dup@x>")
	if _, ok := m.Get(base); !ok {
		t.Errorf("A not recorded at the base mailbox-wide key %q", base)
	}
	qual := state.Qualify(base, EnvelopeSignature(b.Subject, b.SenderEmail, nil, b.Date(), false))
	if _, ok := m.Get(qual); !ok {
		t.Errorf("B not recorded under its #fp-qualified mailbox-wide key %q", qual)
	}
}
