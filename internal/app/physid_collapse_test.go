package app

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-247, R13, R17, S38, S39
// HC2 (collapse × PhysID, rev-6.1 §6): a move-duplicate that the v3→v5 collapse
// already unified into ONE LiveKey survivor — carrying the loser's file path in
// AlsoFiles so redaction still reaches every copy (R13) — gets its PhysID backfilled
// FROM THE LISTING on the first immutable-id-honoring walk, with NO re-download and
// WITHOUT dropping AlsoFiles. No new merge mechanism is needed: the survivor is a
// lone sibling, so the fast-path's listing-backfill (MA-246) applies, and a still-
// live DISTINCT sharer would re-separate via the ordinary distinct-id split (MA-203).
func TestCollapsedSurvivorBackfillsPhysIDKeepsAlsoFiles(t *testing.T) {
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)

	// Pin the sticky store token for "u1" so the seeded survivor uses RunGraph's key.
	srcID := state.MailboxSourceID("u1")
	seed, _ := state.Load(filepath.Join(out, "seed.json"))
	tok := seed.Token(srcID, "u1")

	// A collapsed v6 manifest: ONE survivor at the mailbox-wide key, no PhysID yet,
	// carrying the merged move-duplicate loser's file in AlsoFiles.
	base := state.LiveKey(tok, "mid:m1@x")
	loserRel := filepath.ToSlash(filepath.Join(tok, "Trash", "m1.html"))
	m, _ := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	now := time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC)
	m.Add(base, state.Record{
		Path: filepath.ToSlash(filepath.Join(tok, "Inbox", "m1.html")), Folder: "Inbox",
		Fingerprint: "aaaaaaaaaaaaaaaa", FpScheme: state.FpSchemeCurrent,
		Present: true, FirstFolder: "Inbox", FirstSeen: now, LastSeen: now,
		AlsoFiles: []string{loserRel},
	})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}

	honor := true
	hitsOf, srv := physGraphServer(&honor) // lists M1 (<m1@x>) and M2 in Inbox, honoring immutable ids
	defer srv.Close()
	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"}, BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	if _, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}, logger); err != nil {
		t.Fatalf("walk: %v", err)
	}

	if h := hitsOf(); h["M1"] != 0 {
		t.Errorf("M1 re-downloaded (hits=%d), want 0 — the collapsed survivor must backfill its id from the listing, not re-fetch", h["M1"])
	}
	man := loadManifest(t, out)
	refs := man.KeysForIdentity("mid:m1@x")
	if len(refs) != 1 {
		t.Fatalf("M1 identity has %d records, want 1 (the collapse survivor stays unified)", len(refs))
	}
	rec, _ := man.Get(base)
	if rec.PhysID != "M1" {
		t.Errorf("survivor PhysID = %q, want M1 (backfilled from the listing on the first honoring walk)", rec.PhysID)
	}
	if len(rec.AlsoFiles) != 1 || rec.AlsoFiles[0] != loserRel {
		t.Errorf("survivor AlsoFiles = %v, want [%s] preserved (redaction must still reach the merged loser copy — R13)", rec.AlsoFiles, loserRel)
	}
}
