package export

import (
	"testing"

	"mail-archive-tool/internal/model"
)

// covers: MA-219, R5, S37, S38
// The mailbox-wide skip path emits a folder-assertion on PRESENT-AGAIN (the
// record was gone and is seen again) as well as on a MOVE, even when the folder
// is UNCHANGED. Without the `!prev.Present` twin, a no-Message-ID message — which
// deduples post-download on its content hash and so takes this exporter skip
// path, not the graph pre-download fast-path — that went gone and reappeared in
// the SAME folder got no event, and the fold reported it gone forever after
// (#5, adversarial 2026-09-12; mirrors the fast-path's `|| !rec.Present`).
func TestPresentAgainSameFolderNoMessageID(t *testing.T) {
	out := t.TempDir()
	m := mustManifest(t)
	e := mbwExporter(out, m)
	e.RunAt = testDate

	// A no-Internet-Message-ID message: its identity is a post-download content
	// hash, so a re-observation reaches the mailbox-wide skip, not the fast-path.
	msg := &model.Message{Subject: "No-MID note", Received: testDate, SenderEmail: "a@ex.com", PlainBody: "body text"}

	wrote, err := e.Export("store", []string{"Inbox"}, msg)
	if err != nil || !wrote {
		t.Fatalf("first export: wrote=%v err=%v", wrote, err)
	}
	var key string
	for k := range m.All() {
		key = k
	}
	if key == "" {
		t.Fatalf("no record after the first export")
	}

	// Simulate a gone sweep in the SAME folder: only Present flips.
	if !m.SetPresent(key, false) {
		t.Fatalf("could not flip Present=false on %q", key)
	}

	var called, gotAssert bool
	e.OnManifestSkip = func(k string, folderPath []string, assert bool) { called, gotAssert = true, assert }

	wrote2, err := e.Export("store", []string{"Inbox"}, msg)
	if err != nil {
		t.Fatal(err)
	}
	if wrote2 {
		t.Fatalf("the second export re-wrote a complete, already-archived message (want a mailbox-wide skip)")
	}
	if !called {
		t.Fatalf("OnManifestSkip was not called on the mailbox-wide skip")
	}
	if !gotAssert {
		t.Errorf("present-again in the SAME folder emitted NO folder-assertion (assert=false); the fold would report this no-Message-ID message gone forever after (#5)")
	}
}
