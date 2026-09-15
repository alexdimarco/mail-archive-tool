package export

import (
	"testing"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: MA-242, R17, S38
// HC5 capture-store: on the mailbox-wide (live) path the Exporter records the
// source's immutable id as Record.PhysID; and when a later re-download of the SAME
// message arrives with the id WITHHELD (empty), the exporter carries the
// previously-stored PhysID FORWARD rather than clearing it — so a distinct-reuse
// split still has a PhysID baseline to compare against. PhysID is inert here (never
// folds into identity/fingerprint — MA-239); this proves only the store + carry.
func TestExportCapturesAndCarriesPhysID(t *testing.T) {
	out := t.TempDir()
	m := mustManifest(t)
	e := mbwExporter(out, m)

	// Run 1: an incomplete (no-body) message carrying an immutable id → fillable,
	// so a later run re-writes the record on the same key.
	r1 := &model.Message{Subject: "Q", Received: testDate, InternetMessageID: "<p@x>", SenderEmail: "a@ex.com", PhysID: "IMM-1"}
	if _, err := e.Export("store", []string{"Inbox"}, r1); err != nil {
		t.Fatal(err)
	}
	key := state.LiveKey("store", "mid:<p@x>")
	rec, ok := m.Get(key)
	if !ok || rec.PhysID != "IMM-1" {
		t.Fatalf("after capture: PhysID = %q ok=%v, want IMM-1 stored on the live path", rec.PhysID, ok)
	}

	// Run 2: the SAME message re-downloaded WITH a body (a fill → re-write) but the
	// provider WITHHELD the immutable id this run (empty). The stored id must survive.
	r2 := &model.Message{Subject: "Q", Received: testDate, InternetMessageID: "<p@x>", SenderEmail: "a@ex.com", PlainBody: "the body now arrives"}
	if _, err := e.Export("store", []string{"Inbox"}, r2); err != nil {
		t.Fatal(err)
	}
	rec2, _ := m.Get(key)
	if rec2.PhysID != "IMM-1" {
		t.Errorf("after a withheld-id re-download: PhysID = %q, want the carried-forward IMM-1 (not cleared)", rec2.PhysID)
	}
}
