package app

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-218, S39, R1, R17, R2
// The v3→v4 collapse migration is WIRED into the live path: on the first run
// over a pre-existing v3 archive that holds a per-folder move-duplicate (one
// message recorded under two folders), RunGraph collapses the two records into
// one identity-keyed record, the message is recognised by its LiveKey and NOT
// re-downloaded (R17), and the archive ends with one copy per message (R3). The
// loser file stays on disk (R13). A distinct Message-ID reuser (different
// fingerprint) would be kept — proven by MA-198; here the duplicate shares a
// fingerprint, so it collapses.
func TestGraphV3ToV4CollapseOnFirstUpgradedRun(t *testing.T) {
	out := tmpDir(t)
	f, srv := newFakeGraphServer()
	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"}, BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	opts := Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}
	logger := log.New(io.Discard, "", 0)
	if _, err := RunGraph(context.Background(), g, opts, logger); err != nil {
		srv.Close()
		t.Fatalf("seed run: %v", err)
	}
	srv.Close()

	// Downgrade the archive to a v3 move-duplicate: replace M1's identity-keyed
	// LiveKey record with TWO folder-scoped v3 records (Inbox + Trash), same
	// fingerprint, and mark the manifest version 3 so the next run's collapse
	// fires. M2/M3 keep their LiveKeys (already-collapsed, left verbatim).
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	raw, err := os.ReadFile(mpath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["version"] = 3
	entries := doc["entries"].(map[string]interface{})
	var liveKey string
	for k := range entries {
		if strings.HasSuffix(k, "mid:m1@x") {
			liveKey = k
		}
	}
	if liveKey == "" {
		t.Fatalf("no M1 record in entries: %v keys", len(entries))
	}
	token := strings.Split(liveKey, "\x00")[0]
	rec := entries[liveKey].(map[string]interface{})
	origPath, _ := rec["path"].(string)
	// The surviving (Inbox) folder-scoped record keeps M1's real file.
	inbox := map[string]interface{}{}
	for k, v := range rec {
		inbox[k] = v
	}
	inbox["folder"] = "Inbox"
	inbox["path"] = origPath
	// The loser (Trash) record points at a second real file on disk.
	trashRel := filepath.ToSlash(filepath.Join(token, "Trash", "m1-trash.html"))
	if err := os.MkdirAll(filepath.Join(out, token, "Trash"), 0o755); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(out, filepath.FromSlash(origPath)))
	if err := os.WriteFile(filepath.Join(out, filepath.FromSlash(trashRel)), body, 0o644); err != nil {
		t.Fatal(err)
	}
	trash := map[string]interface{}{}
	for k, v := range rec {
		trash[k] = v
	}
	trash["folder"] = "Trash"
	trash["path"] = trashRel
	delete(entries, liveKey)
	entries[token+"\x00Inbox\x00mid:m1@x"] = inbox
	entries[token+"\x00Trash\x00mid:m1@x"] = trash
	out2, _ := json.Marshal(doc)
	if err := os.WriteFile(mpath, out2, 0o600); err != nil {
		t.Fatal(err)
	}

	// The upgraded run: fresh server (hits reset to 0), same archive.
	f2, srv2 := newFakeGraphServer()
	defer srv2.Close()
	_ = f
	g.BaseURL = srv2.URL
	g.TokenURL = srv2.URL + "/token"
	if _, err := RunGraph(context.Background(), g, opts, logger); err != nil {
		t.Fatalf("upgraded run: %v", err)
	}

	// M1 was recognised by its LiveKey after the collapse — not re-downloaded.
	if hits := f2.hits(); hits["M1"] != 0 {
		t.Errorf("M1 was re-downloaded on the first upgraded run (hits=%d) — the v3→v4 collapse did not run or did not key by identity", hits["M1"])
	}
	// Exactly one record for M1's identity remains.
	m, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(m.KeysForIdentity("mid:m1@x")); n != 1 {
		t.Errorf("M1 identity has %d records after the upgraded run, want 1 (collapse did not unify the move-duplicate)", n)
	}
	// The loser file is kept on disk (R13).
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(trashRel))); err != nil {
		t.Errorf("collapse deleted the loser file (R13 violated): %v", err)
	}
}
