package export

import (
	"testing"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: MA-231, R1, R3, S39
// Adopt-never-split applies ONLY when the identity has a single same-store record
// (the unambiguous single-message migration). When a collapse left TWO distinct
// messages reusing one Message-ID as siblings, a full-mode re-export of one must
// NOT adopt-overwrite (drop) the other (R1): with >1 record the guard is off and
// the message #fp-splits by content instead, so every original survives.
func TestFullModeAdoptDoesNotDropSiblingOnReuse(t *testing.T) {
	out := t.TempDir()
	m := mustManifest(t)
	e := incExporter(out, m)
	e.DedupMailboxWide = true
	e.Mode = Full

	base := state.LiveKey("store", "mid:<x@x>")
	qual := state.Qualify(base, "bbbbbbbbbbbbbbbb")
	// Two DISTINCT legacy-scheme messages that reused one Message-ID (FpScheme 0).
	m.Add(base, state.Record{Path: "store/Inbox/a.html", Folder: "Inbox", FirstFolder: "Inbox", Present: true, Fingerprint: "aaaaaaaaaaaaaaaa", FpScheme: 0})
	m.Add(qual, state.Record{Path: "store/Sent/b.html", Folder: "Sent", FirstFolder: "Sent", Present: true, Fingerprint: "bbbbbbbbbbbbbbbb", FpScheme: 0})

	msg := &model.Message{Subject: "reuse", Received: testDate, SenderEmail: "a@ex.com", InternetMessageID: "<x@x>", PlainBody: "a full-mode re-export of one of them"}
	if _, err := e.Export("store", []string{"Inbox"}, msg); err != nil {
		t.Fatal(err)
	}

	// Both original legacy fingerprints must still be present — neither dropped.
	have := map[string]bool{}
	for _, ref := range m.KeysForIdentity("mid:<x@x>") {
		if r, ok := m.Get(ref.Key); ok {
			have[r.Fingerprint] = true
		}
	}
	if !have["aaaaaaaaaaaaaaaa"] {
		t.Errorf("the base sibling (fp aaaa) was OVERWRITTEN by a full-mode adopt — a distinct message was dropped (R1)")
	}
	if !have["bbbbbbbbbbbbbbbb"] {
		t.Errorf("the #fp-qualified sibling (fp bbbb) was dropped")
	}
}
