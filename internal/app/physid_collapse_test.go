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
// HC2 × rev-6.1 (content-hash arbiter): a move-duplicate the v3→v5 collapse unified
// into ONE LiveKey survivor is a PRE-v6/floor record — it carries NO content hash
// (and the loser's file in AlsoFiles for redaction reach, R13). On the first
// immutable-id-honoring walk the closure does NOT engage against it: with no
// comparable content hash it cannot tell a distinct reuse from the archived message,
// so it stays the shipped Message-ID floor — the survivor is skipped by membership
// (NO re-download), its AlsoFiles preserved, and its PhysID left UNSET (no unsafe
// id backfill, no storm). Such an archive closes #8 only for messages captured
// fresh under v6.
func TestCollapsedSurvivorStaysFloorNoContentHash(t *testing.T) {
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)

	srcID := state.MailboxSourceID("u1")
	seed, _ := state.Load(filepath.Join(out, "seed.json"))
	tok := seed.Token(srcID, "u1")

	base := state.LiveKey(tok, "mid:m1@x")
	loserRel := filepath.ToSlash(filepath.Join(tok, "Trash", "m1.html"))
	m, _ := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	now := time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC)
	// A collapsed survivor as a v3→v5 collapse leaves it: fingerprint set, but NO
	// PhysID and NO ContentHash (both are v6 fields a pre-v6 collapse never wrote).
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
	hitsOf, srv := physGraphServer(&honor) // lists M1 (<m1@x>) and M2, honoring immutable ids
	defer srv.Close()
	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"}, BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	if _, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}, logger); err != nil {
		t.Fatalf("walk: %v", err)
	}

	if h := hitsOf(); h["M1"] != 0 {
		t.Errorf("M1 re-downloaded (hits=%d), want 0 — a pre-v6 collapsed survivor must stay the membership floor, not re-fetch", h["M1"])
	}
	man := loadManifest(t, out)
	refs := man.KeysForIdentity("mid:m1@x")
	if len(refs) != 1 {
		t.Fatalf("M1 identity has %d records, want 1 (the collapse survivor stays unified on the floor)", len(refs))
	}
	rec, _ := man.Get(base)
	if rec.PhysID != "" {
		t.Errorf("survivor PhysID = %q, want empty — the closure must NOT bind a listed id to a record it cannot content-verify (HC4)", rec.PhysID)
	}
	if len(rec.AlsoFiles) != 1 || rec.AlsoFiles[0] != loserRel {
		t.Errorf("survivor AlsoFiles = %v, want [%s] preserved (redaction must still reach the merged loser copy — R13)", rec.AlsoFiles, loserRel)
	}
}
