package app

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/pages"
	"mail-archive-tool/internal/state"
)

func reindexMsg(subject, id string) *model.Message {
	return &model.Message{
		Subject:           subject,
		SenderName:        "Sender",
		SenderEmail:       "sender@example.com",
		To:                "me@example.com",
		Received:          time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC),
		InternetMessageID: id,
		HTMLBody:          "<p>" + subject + " body</p>",
	}
}

// buildArchive writes a few messages into out with a live search index,
// manifest, and folder pages — exactly the artifacts a real export produces. It
// returns the exported html paths (relative to out) keyed by subject.
func buildArchive(t *testing.T, out string) map[string]string {
	t.Helper()
	idx, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	rels := map[string]string{}
	exp := &export.Exporter{
		OutDir:   out,
		Manifest: manifest,
		Mode:     export.Full,
		Log:      log.New(io.Discard, "", 0),
		OnExported: func(store string, fp []string, m *model.Message, rel, key string) {
			rels[m.Subject] = rel
			if addErr := idx.Add(store, fp, m, rel, key); addErr != nil {
				t.Fatalf("index add: %v", addErr)
			}
		},
	}

	msgs := []struct {
		folder []string
		m      *model.Message
	}{
		{[]string{"Inbox"}, reindexMsg("keep-alpha", "<alpha@x>")},
		{[]string{"Inbox"}, reindexMsg("prune-beta", "<beta@x>")},
		{[]string{"Archive"}, reindexMsg("keep-gamma", "<gamma@x>")},
	}
	for _, mm := range msgs {
		if _, err := exp.Export("Store", mm.folder, mm.m); err != nil {
			t.Fatalf("export %s: %v", mm.m.Subject, err)
		}
	}
	if err := idx.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := pages.Generate(out, idx, log.New(io.Discard, "", 0)); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(); err != nil {
		t.Fatal(err)
	}
	if len(rels) != 3 {
		t.Fatalf("expected 3 exported messages, got %d", len(rels))
	}
	return rels
}

func indexTotal(t *testing.T, out, query string) int {
	t.Helper()
	ix, err := index.OpenReadonly(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	_, total, err := ix.Search(index.Query{Text: query})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

// covers: MA-40, MA-41, MA-42, R13, S13
// Reindex reconciles the archive to disk: after an exported file is deleted, its
// row is pruned from the index and its entry from the manifest (MA-40), the
// surviving files stay searchable (MA-41), and the folder page is regenerated so
// it no longer lists the pruned message (MA-42).
func TestReindexReconciles(t *testing.T) {
	out := t.TempDir()
	rels := buildArchive(t, out)

	// Sanity: everything is present and searchable before we break anything.
	if got := indexTotal(t, out, "keep"); got != 2 {
		t.Fatalf("pre-reindex 'keep' matches = %d, want 2", got)
	}
	if got := indexTotal(t, out, "prune"); got != 1 {
		t.Fatalf("pre-reindex 'prune' matches = %d, want 1", got)
	}

	// A user deletes one exported message file out from under the archive.
	prunedRel := rels["prune-beta"]
	if err := os.Remove(filepath.Join(out, filepath.FromSlash(prunedRel))); err != nil {
		t.Fatal(err)
	}

	kept, pruned, err := Reindex(out, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if kept != 2 || pruned != 1 {
		t.Errorf("Reindex kept=%d pruned=%d, want kept=2 pruned=1", kept, pruned)
	}

	// MA-40: the pruned message is gone from the index; MA-41: survivors remain.
	if got := indexTotal(t, out, "prune"); got != 0 {
		t.Errorf("post-reindex 'prune' matches = %d, want 0 (dangling row not pruned)", got)
	}
	if got := indexTotal(t, out, "keep"); got != 2 {
		t.Errorf("post-reindex 'keep' matches = %d, want 2 (survivors dropped)", got)
	}

	// MA-40: the manifest entry for the pruned message is gone; survivors stay.
	manifest, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	betaKey := state.Key("Inbox", reindexMsg("prune-beta", "<beta@x>").Identity())
	if manifest.Has(betaKey) {
		t.Error("manifest still records the pruned message")
	}
	if manifest.Len() != 2 {
		t.Errorf("manifest len after reindex = %d, want 2", manifest.Len())
	}

	// MA-42: the Inbox folder page was regenerated and no longer lists the pruned
	// file, but still lists the surviving one. Exports nest under <store>/, so the
	// folder page lives in the pruned file's own directory.
	pageDir := filepath.Dir(filepath.FromSlash(prunedRel))
	page, err := os.ReadFile(filepath.Join(out, pageDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	prunedBase := filepath.Base(prunedRel)
	keptBase := filepath.Base(rels["keep-alpha"])
	if strings.Contains(string(page), prunedBase) {
		t.Errorf("regenerated folder page still references pruned file %q", prunedBase)
	}
	if !strings.Contains(string(page), keptBase) {
		t.Errorf("regenerated folder page dropped surviving file %q", keptBase)
	}
}
