package export

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: MA-121, R11
// The exporter's -since date filter excludes exactly the items outside the
// window: an unseen message dated before -since is skipped (SkippedDate counts
// it, nothing is written, nothing is recorded in the manifest) while one dated
// on/after the window is exported. Positive twin first — the on/after message
// lands as a real file and a manifest record — so the exclusion is proven to
// fire for the date, not for some other reason.
func TestDateFilterExcludesExactlyOutsideWindow(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	since := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	e := incExporter(out, manifest)
	e.Since = since

	// Positive twin: a healthy message dated on the window boundary is exported.
	after := &model.Message{Subject: "Kept", Received: since, InternetMessageID: "<after@x>", PlainBody: "in window"}
	wrote, err := e.Export("store", []string{"Inbox"}, after)
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, wrote, "the on/after message is exported")
	afterKey := state.Key("store", "Inbox", after.Identity())
	rec, ok := manifest.Get(afterKey)
	if !ok {
		t.Fatal("the on/after message was not recorded in the manifest")
	}
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rec.Path))); err != nil {
		t.Fatalf("the on/after message wrote no file: %v", err)
	}

	// Excluded: a message dated one day before -since is skipped, unwritten,
	// unrecorded.
	before := &model.Message{Subject: "Dropped", Received: since.Add(-24 * time.Hour),
		InternetMessageID: "<before@x>", PlainBody: "before window"}
	wrote, err = e.Export("store", []string{"Inbox"}, before)
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Error("a message dated before -since must not be exported")
	}
	if e.Stats.SkippedDate != 1 {
		t.Errorf("SkippedDate = %d, want 1", e.Stats.SkippedDate)
	}
	if e.Stats.Exported != 1 {
		t.Errorf("Exported = %d, want 1 (only the on/after message)", e.Stats.Exported)
	}
	if _, seen := manifest.Get(state.Key("store", "Inbox", before.Identity())); seen {
		t.Error("a message excluded by -since must not be recorded in the manifest")
	}
	if n := countSuffix(t, out, ".html"); n != 1 {
		t.Errorf("html files on disk = %d, want 1 (the excluded message wrote nothing)", n)
	}
}
