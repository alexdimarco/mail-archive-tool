package app

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-229, R21, R1, S39
// EC5 — the membership skip must stamp EVERY same-identity sibling, not just the
// base. A collapsed archive can hold two DISTINCT messages that reused one
// Internet-Message-ID as #fp-qualified siblings, both present in a walked folder.
// A run observes the identity once (one listing entry) but cannot say which
// physical sibling it is, so it advances LastSeen on ALL of them; otherwise the
// un-stamped sibling would be phantom-marked gone by SweepGone while it is
// physically present — a durable R21 falsification of an already-archived message.
func TestMembershipSkipStampsAllSiblingsNoPhantomGone(t *testing.T) {
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	seed, _ := state.Load(filepath.Join(out, "seed.json"))
	tok := seed.Token(state.MailboxSourceID("u1"), "u1")
	base := state.LiveKey(tok, "mid:m1@x")
	qual := state.Qualify(base, "bbbbbbbbbbbbbbbb")
	baseRel := filepath.ToSlash(filepath.Join(tok, "Inbox", "m1a.html"))
	qualRel := filepath.ToSlash(filepath.Join(tok, "Inbox", "m1b.html"))
	for _, rel := range []string{baseRel, qualRel} {
		p := filepath.Join(out, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// v5 archive, token already marked collapsed (so this tests the fast-path
	// stamping, not the collapse): two present same-identity siblings, both in the
	// walked Inbox, both last seen long ago.
	doc := struct {
		Version         int                     `json:"version"`
		Stores          map[string]string       `json:"stores"`
		CollapsedTokens map[string]bool         `json:"collapsed_tokens"`
		Entries         map[string]state.Record `json:"entries"`
	}{
		Version:         5,
		Stores:          map[string]string{state.MailboxSourceID("u1"): tok},
		CollapsedTokens: map[string]bool{tok: true},
		Entries: map[string]state.Record{
			base: {Path: baseRel, Folder: "Inbox", FirstFolder: "Inbox", FirstSeen: old, LastSeen: old, Present: true, Fingerprint: "aaaaaaaaaaaaaaaa", FpScheme: state.FpSchemeCurrent},
			qual: {Path: qualRel, Folder: "Inbox", FirstFolder: "Inbox", FirstSeen: old, LastSeen: old, Present: true, Fingerprint: "bbbbbbbbbbbbbbbb", FpScheme: state.FpSchemeCurrent},
		},
	}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	f, srv := newFakeGraphServer()
	defer srv.Close()
	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"},
		BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	if _, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Index: false}, logger); err != nil {
		t.Fatalf("run: %v", err)
	}
	_ = f

	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	rb, _ := m.Get(base)
	rq, _ := m.Get(qual)
	if !rb.Present {
		t.Errorf("the base sibling was marked gone (it is physically present)")
	}
	if !rq.Present {
		t.Errorf("the #fp-qualified sibling was PHANTOM-marked gone though physically present — EC5 stamping failed (R21)")
	}
}
