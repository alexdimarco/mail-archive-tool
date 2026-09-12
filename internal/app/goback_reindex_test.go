package app

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-214, R21, S37, R13
// reindex compacts the go-back history log so redaction spans all dates: after
// pruning the index/manifest rows of files gone from disk, it drops every
// per-message event whose key is no longer in the reconciled manifest (a message
// removed from the archive), while KEEPING run headers/footers, folder renames,
// and the events of a message merely DEPARTED from the live mailbox whose file
// is still on disk (a gone event with its file kept — R13). A message on disk is
// untouched by compaction.
func TestReindexCompactsRedactedHistory(t *testing.T) {
	out := tmpDir(t)
	rels := buildArchive(t, out) // keep-alpha, prune-beta (redacted below), keep-gamma

	// Recover the real manifest keys for the exported files.
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	keyOf := func(subject string) string {
		k, ok := m.KeyForPath(rels[subject])
		if !ok {
			t.Fatalf("no manifest key for %q (%s)", subject, rels[subject])
		}
		return k
	}
	kAlpha, kBeta, kGamma := keyOf("keep-alpha"), keyOf("prune-beta"), keyOf("keep-gamma")

	// A timeline over the three: all seen at D1; keep-gamma later departs the
	// mailbox (a gone event) while its file stays on disk.
	d1 := time.Date(2025, 4, 1, 9, 0, 0, 0, time.UTC)
	d2 := d1.Add(24 * time.Hour)
	hpath := filepath.Join(out, state.HistoryName)
	w, err := state.OpenHistory(hpath)
	if err != nil {
		t.Fatal(err)
	}
	mustW := func(e error) {
		if e != nil {
			t.Fatal(e)
		}
	}
	mustW(w.WriteRunHeader(1, d1, []string{"Store"}))
	mustW(w.WriteFolder(kAlpha, "Inbox"))
	mustW(w.WriteFolder(kBeta, "Inbox"))
	mustW(w.WriteFolder(kGamma, "Archive"))
	mustW(w.WriteRunFooter(1, d1))
	mustW(w.WriteRunHeader(2, d2, []string{"Store"}))
	mustW(w.WriteGone(kGamma)) // departed, but its file is kept (R13)
	mustW(w.WriteRunFooter(2, d2))
	mustW(w.Close())

	// Redact prune-beta: delete its exported file (its manifest row and history
	// events still exist until reindex reconciles).
	if err := os.Remove(filepath.Join(out, filepath.FromSlash(rels["prune-beta"]))); err != nil {
		t.Fatal(err)
	}

	kept, pruned, err := Reindex(out, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if kept != 2 || pruned != 1 {
		t.Fatalf("reindex kept=%d pruned=%d, want kept=2 pruned=1", kept, pruned)
	}

	events, err := state.ReadHistory(hpath)
	if err != nil {
		t.Fatal(err)
	}
	var betaEvents, alphaEvents, gammaEvents, gammaGone, runHeaders int
	for _, ev := range events {
		switch {
		case ev.Run > 0 && ev.At != "":
			runHeaders++
		case ev.K == kBeta:
			betaEvents++
		case ev.K == kAlpha:
			alphaEvents++
		case ev.K == kGamma:
			gammaEvents++
			if ev.Gone {
				gammaGone++
			}
		}
	}
	if betaEvents != 0 {
		t.Errorf("the redacted message's history events survived compaction (%d) — redaction does not span all dates (R21)", betaEvents)
	}
	if alphaEvents == 0 {
		t.Errorf("compaction dropped a present message's events")
	}
	if gammaEvents == 0 || gammaGone == 0 {
		t.Errorf("compaction dropped a DEPARTED-but-on-disk message's events (gamma events=%d gone=%d) — R13: its file and timeline are kept", gammaEvents, gammaGone)
	}
	if runHeaders != 2 {
		t.Errorf("run headers must survive compaction (the date track), got %d want 2", runHeaders)
	}

	// The manifest no longer carries the redacted key, but still carries the two
	// on-disk records.
	m2, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if m2.Has(kBeta) {
		t.Errorf("reindex left the redacted record in the manifest")
	}
	if !m2.Has(kAlpha) || !m2.Has(kGamma) {
		t.Errorf("reindex pruned a record whose file is still on disk")
	}
}
