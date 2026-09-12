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

// covers: MA-226, R17, R3, R1, S39
// The option-D v3→v5 upgrade end to end (DC13 #1), from a GENUINE v3 fixture (not
// a reshaped v4 record — the vacuous-test trap the earlier attempt fell into): a
// v3 Graph archive holds message M1 as a per-folder MOVE-DUPLICATE (Inbox + Trash,
// two folder-scoped keys, same legacy fingerprint) with real files and a v3 search
// index. The first upgraded RunGraph collapses the two into one mailbox-wide
// LiveKey record BEFORE the walk and re-keys the index; the walk then recognises
// M1 by Message-ID MEMBERSHIP and does NOT re-download it (R17), the archive holds
// ONE record for the identity (R3), and the index carries exactly one row for it.
func TestGraphV3MoveDuplicateUpgradeNoRedownload(t *testing.T) {
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)

	// Learn the sticky store token for mailbox "u1" (deterministic per display
	// name), and pin it into the v3 fixture's Stores so RunGraph reuses it.
	srcID := state.MailboxSourceID("u1")
	seed, _ := state.Load(filepath.Join(out, "seed.json"))
	tok := seed.Token(srcID, "u1")

	inboxKey := state.Key(tok, "Inbox", "mid:m1@x") // first-captured copy
	trashKey := state.Key(tok, "Trash", "mid:m1@x") // moved-to-trash duplicate
	inboxRel := filepath.ToSlash(filepath.Join(tok, "Inbox", "m1.html"))
	trashRel := filepath.ToSlash(filepath.Join(tok, "Trash", "m1.html"))
	for _, rel := range []string{inboxRel, trashRel} {
		p := filepath.Join(out, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("<html>m1</html>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// A REAL version-3 manifest: folder-scoped keys, a shared legacy fingerprint
	// (FpScheme omitted → 0/legacy), no timeline fields.
	doc := struct {
		Version int                     `json:"version"`
		Stores  map[string]string       `json:"stores"`
		Entries map[string]state.Record `json:"entries"`
	}{
		Version: 3,
		Stores:  map[string]string{srcID: tok},
		Entries: map[string]state.Record{
			inboxKey: {Path: inboxRel, Folder: "Inbox", Fingerprint: "aaaaaaaaaaaaaaaa"},
			trashKey: {Path: trashRel, Folder: "Trash", Fingerprint: "aaaaaaaaaaaaaaaa"},
		},
	}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	// A v3 search index with the two folder-scoped rows.
	ix, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	m1 := &model.Message{Subject: "subj-M1", SenderEmail: "a@example.com", InternetMessageID: "<m1@x>"}
	if err := ix.Add(tok, []string{"Inbox"}, m1, inboxRel, inboxKey); err != nil {
		t.Fatal(err)
	}
	if err := ix.Add(tok, []string{"Trash"}, m1, trashRel, trashKey); err != nil {
		t.Fatal(err)
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}
	ix.Close()

	// The first upgraded run over the fake mailbox (M1 in Inbox, plus M2/M3).
	f, srv := newFakeGraphServer()
	defer srv.Close()
	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"},
		BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	if _, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}, logger); err != nil {
		t.Fatalf("upgraded run: %v", err)
	}

	// M1 was recognised by membership after the collapse — NOT re-downloaded.
	if h := f.hits()["M1"]; h != 0 {
		t.Errorf("M1 was re-downloaded on the first upgraded run (hits=%d), want 0 — the collapse+membership skip failed", h)
	}
	// Exactly one record for M1's identity, at the collapsed LiveKey.
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	refs := m.KeysForIdentity("mid:m1@x")
	if len(refs) != 1 {
		t.Fatalf("M1 identity has %d records after upgrade, want 1 (collapse did not unify the move-duplicate)", len(refs))
	}
	base := state.LiveKey(tok, "mid:m1@x")
	rec, ok := m.Get(base)
	if !ok {
		t.Errorf("M1 not at the collapsed live key %q", base)
	}
	if rec.Path != inboxRel {
		t.Errorf("survivor kept %q, want the first-captured %q", rec.Path, inboxRel)
	}
	// (The index re-key itself — survivor renamed, loser dropped — is proven by
	// the idx.Rekey unit test, MA-225.)
}
