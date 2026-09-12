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
	"mail-archive-tool/internal/state"
)

// covers: MA-238, R13, R1, S39
// A full-mode Graph run must NEVER delete the file of an already-archived
// legacy-scheme message when a message with the same Message-ID is re-observed:
// the legacy record's identity is not content-verified, so the observed message
// might be a DISTINCT reuse of a gone original, and deleting the original's file
// would be irreversible data loss (R13). The file is kept (a later verify flags
// the orphan); the reuse-vs-same ambiguity is fully resolved only by the deferred
// ImmutableId closure.
func TestFullModeReuseKeepsLegacyOriginalFile(t *testing.T) {
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)
	seed, _ := state.Load(filepath.Join(out, "seed.json"))
	tok := seed.Token(state.MailboxSourceID("u1"), "u1")

	// A legacy-scheme original M at the base live key, filed under a DISTINCT stem
	// (so a re-export to a new stem would trigger the rename-cleanup delete), gone
	// from the live mailbox.
	base := state.LiveKey(tok, "mid:m1@x")
	oldRel := filepath.ToSlash(filepath.Join(tok, "Inbox", "old-m.html"))
	op := filepath.Join(out, filepath.FromSlash(oldRel))
	_ = os.MkdirAll(filepath.Dir(op), 0o755)
	if err := os.WriteFile(op, []byte("<html>the ORIGINAL M</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := struct {
		Version         int                     `json:"version"`
		Stores          map[string]string       `json:"stores"`
		CollapsedTokens map[string]bool         `json:"collapsed_tokens"`
		Entries         map[string]state.Record `json:"entries"`
	}{
		Version: 5, Stores: map[string]string{state.MailboxSourceID("u1"): tok},
		CollapsedTokens: map[string]bool{tok: true}, // no collapse — exercise the exporter directly
		Entries: map[string]state.Record{
			base: {Path: oldRel, Folder: "Inbox", FirstFolder: "Inbox", Present: false, Fingerprint: "old0old0old0old0", FpScheme: 0},
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
	// FULL mode: bypasses the membership skip, so the reused-id message reaches Export.
	if _, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Full, Index: false}, logger); err != nil {
		t.Fatalf("run: %v", err)
	}
	_ = f

	if _, err := os.Stat(op); err != nil {
		t.Errorf("the legacy original's file was DELETED by a full-mode reuse (irreversible data loss, R13): %v", err)
	}
}
