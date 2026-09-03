package export

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/model"
)

// covers: MA-128, R6, R3, R2, S30
// Two mailboxes whose stores share one display name — Thunderbird's "Local
// Folders" under every profile, Outlook's default "Outlook Data File" — archived
// into one -out must not collide. The token registry hands the second store a
// distinct, sticky token (segment~hash), so the SAME mail (same Message-ID, same
// folder) held in both stores is exported to BOTH trees (R6/R3), never lost as
// "skipped(seen)"; an incremental re-run over the unchanged sources exports zero
// (R2). Without the injective token the second copy would silently vanish.
func TestSameNamedStoresGetDistinctTrees(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)

	srcA := "/home/u/.thunderbird/aaaa.default/Mail/Local Folders"
	srcB := "/home/u/.thunderbird/bbbb.default/Mail/Local Folders"
	tokA := manifest.Token(srcA, "Local Folders")
	tokB := manifest.Token(srcB, "Local Folders")
	if tokA == tokB {
		t.Fatalf("same-named stores collapsed to one token %q — the second copy would be lost", tokA)
	}
	if tokA != "Local Folders" {
		t.Errorf("first claimant token = %q, want the plain segment %q", tokA, "Local Folders")
	}
	if !strings.HasPrefix(tokB, "Local Folders~") {
		t.Errorf("second store token = %q, want a disambiguated \"Local Folders~<hash>\"", tokB)
	}
	// The token is sticky: asked again for the same source it does not change.
	if again := manifest.Token(srcB, "Local Folders"); again != tokB {
		t.Errorf("token not sticky: %q then %q", tokB, again)
	}

	msg := func() *model.Message {
		return &model.Message{Subject: "Statement", Received: testDate, InternetMessageID: "<same@x>", PlainBody: "hi"}
	}
	e := incExporter(out, manifest)
	for _, tok := range []string{tokA, tokB} {
		wrote, err := e.Export(tok, []string{"Inbox"}, msg())
		if err != nil {
			t.Fatal(err)
		}
		if !wrote {
			t.Fatalf("store %q did not export the shared message (collision)", tok)
		}
	}
	if got := countSuffix(t, out, ".html"); got != 2 {
		t.Fatalf("shared message exported to %d files, want 2 (one per store)", got)
	}
	for _, tok := range []string{tokA, tokB} {
		if got := countSuffix(t, filepath.Join(out, tok), ".html"); got != 1 {
			t.Errorf("store tree %q holds %d html files, want 1", tok, got)
		}
	}

	e2 := incExporter(out, manifest)
	for _, tok := range []string{tokA, tokB} {
		if _, err := e2.Export(tok, []string{"Inbox"}, msg()); err != nil {
			t.Fatal(err)
		}
	}
	if e2.Stats.Exported != 0 || e2.Stats.SkippedManifest != 2 {
		t.Errorf("incremental re-run: exported=%d skipped=%d, want 0/2", e2.Stats.Exported, e2.Stats.SkippedManifest)
	}
}

// covers: MA-129, R6, R2, S30
// Two distinctly-named stores each keep their plain sanitized segment as token,
// export into separate top-level trees, and an incremental re-run exports zero
// — the ordinary two-store case, now store-scoped rather than folder-scoped.
func TestDistinctNamedStoresGetSeparateTrees(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)

	tokA := manifest.Token("/data/alpha.pst", "Alpha Archive")
	tokB := manifest.Token("/data/beta.pst", "Beta Archive")
	if tokA != "Alpha Archive" || tokB != "Beta Archive" {
		t.Fatalf("distinct display names not preserved as plain tokens: %q / %q", tokA, tokB)
	}

	msg := func(id string) *model.Message {
		return &model.Message{Subject: "s", Received: testDate, InternetMessageID: id, PlainBody: "b"}
	}
	e := incExporter(out, manifest)
	if _, err := e.Export(tokA, []string{"Inbox"}, msg("<a@x>")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Export(tokB, []string{"Inbox"}, msg("<b@x>")); err != nil {
		t.Fatal(err)
	}
	if got := countSuffix(t, filepath.Join(out, "Alpha Archive"), ".html"); got != 1 {
		t.Errorf("Alpha tree = %d html files, want 1", got)
	}
	if got := countSuffix(t, filepath.Join(out, "Beta Archive"), ".html"); got != 1 {
		t.Errorf("Beta tree = %d html files, want 1", got)
	}

	e2 := incExporter(out, manifest)
	if _, err := e2.Export(tokA, []string{"Inbox"}, msg("<a@x>")); err != nil {
		t.Fatal(err)
	}
	if _, err := e2.Export(tokB, []string{"Inbox"}, msg("<b@x>")); err != nil {
		t.Fatal(err)
	}
	if e2.Stats.Exported != 0 || e2.Stats.SkippedManifest != 2 {
		t.Errorf("incremental re-run: exported=%d skipped=%d, want 0/2", e2.Stats.Exported, e2.Stats.SkippedManifest)
	}
}
