package app

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: MA-237, R3, R8, S39
// The one-time collapse is DEFERRED when a search index exists on disk but is not
// open this run (-index=false): collapsing would re-key the manifest to LiveKeys
// while leaving the index at its v3 folder-scoped keys — a permanent desync that
// a later reindex redaction could not reconcile. So the token still OWES the
// collapse (it is not marked), and an -index run performs it, re-keying both in
// step.
func TestCollapseDeferredWhenIndexPresentButClosed(t *testing.T) {
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)
	srcID := state.MailboxSourceID("u1")
	seed, _ := state.Load(filepath.Join(out, "seed.json"))
	tok := seed.Token(srcID, "u1")

	k := state.Key(tok, "Inbox", "mid:m1@x")
	rel := filepath.ToSlash(filepath.Join(tok, "Inbox", "m1.html"))
	p := filepath.Join(out, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte("<html>m1</html>"), 0o644)

	doc := struct {
		Version int                     `json:"version"`
		Stores  map[string]string       `json:"stores"`
		Entries map[string]state.Record `json:"entries"`
	}{Version: 3, Stores: map[string]string{srcID: tok}, Entries: map[string]state.Record{
		k: {Path: rel, Folder: "Inbox", Fingerprint: "aaaaaaaaaaaaaaaa"},
	}}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	// A v3 search index EXISTS on disk (folder-scoped row).
	ix, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.Add(tok, []string{"Inbox"}, &model.Message{Subject: "subj-M1", InternetMessageID: "<m1@x>"}, rel, k); err != nil {
		t.Fatal(err)
	}
	_ = ix.Flush()
	ix.Close()

	f, srv := newFakeGraphServer()
	defer srv.Close()
	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"},
		BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	// Run WITHOUT the index open.
	if _, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Index: false}, logger); err != nil {
		t.Fatalf("run: %v", err)
	}
	_ = f

	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !m.OwesCollapse(tok) {
		t.Errorf("the collapse was performed with -index=false while a search index exists — the token was marked collapsed, desyncing the un-re-keyed v3 index (R3/R8)")
	}
}
