package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// stripStoreSegment drops the first NUL-delimited segment from a key, turning a
// v3 (token\x00folder\x00id) key back into the v2 (folder\x00id) shape an
// already-shipped binary writes.
func stripStoreSegment(key string) string {
	if i := strings.IndexByte(key, '\x00'); i >= 0 {
		return key[i+1:]
	}
	return key
}

// countTree counts files with the given suffix anywhere under root.
func countTree(t *testing.T, root, suffix string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), suffix) {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// buildThenDowngrade runs a real export into a fresh archive, then rewrites its
// manifest and search index into the pre-store-scoped shape an already-shipped
// binary would leave: a version-2 manifest with one-NUL keys and no stores map,
// and a pre-meta index (no meta table) with one-NUL rows. It is the fixture the
// first-open upgrade must migrate. Returns the archive dir, its manifest path,
// and the source dir so the caller can re-run the same export.
func buildThenDowngrade(t *testing.T) (out, mpath, src string) {
	t.Helper()
	src = filepath.Join(tmpDir(t), "Mailbox")
	maildirMessages(t, src, 2)
	out = tmpDir(t)
	mpath = filepath.Join(out, ".mailarchive-manifest.json")

	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatalf("build run: %v", err)
	}
	downgradeManifest(t, mpath)
	downgradeIndex(t, filepath.Join(out, "search.db"))
	return out, mpath, src
}

func downgradeManifest(t *testing.T, mpath string) {
	t.Helper()
	raw, err := os.ReadFile(mpath)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version int                        `json:"version"`
		Stores  map[string]string          `json:"stores,omitempty"`
		Entries map[string]json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Entries) == 0 {
		t.Fatal("build produced no manifest entries to downgrade")
	}
	down := make(map[string]json.RawMessage, len(doc.Entries))
	for k, v := range doc.Entries {
		down[stripStoreSegment(k)] = v
	}
	doc.Version = 2
	doc.Stores = nil
	doc.Entries = down
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mpath, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func downgradeIndex(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP TABLE IF EXISTS meta`); err != nil {
		t.Fatal(err)
	}
	type row struct {
		id  int64
		key string
	}
	var rows []row
	rs, err := db.Query(`SELECT id, key FROM docs`)
	if err != nil {
		t.Fatal(err)
	}
	for rs.Next() {
		var r row
		if err := rs.Scan(&r.id, &r.key); err != nil {
			rs.Close()
			t.Fatal(err)
		}
		rows = append(rows, r)
	}
	rs.Close()
	if len(rows) == 0 {
		t.Fatal("build produced no index rows to downgrade")
	}
	for _, r := range rows {
		if _, err := db.Exec(`UPDATE docs SET key=? WHERE id=?`, stripStoreSegment(r.key), r.id); err != nil {
			t.Fatal(err)
		}
	}
}

// covers: MA-130, R5, R2, R8, S30
// An archive whose manifest and search index predate store-scoped keys is
// migrated on the first open: the load re-scopes the manifest (by content, from
// each record's own path) and RepairKeys re-scopes the index; the re-run then
// finds every message already recorded and exports zero — proving the migrated
// keys match the token a fresh run computes, so no message is silently
// re-exported into a duplicate tree. A second load re-scopes nothing (idempotent).
func TestUpgradeMigratesAndReExportsNothing(t *testing.T) {
	out, mpath, src := buildThenDowngrade(t)
	htmlBefore := countTree(t, out, ".html")

	res, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true}, log.New(io.Discard, "", 0), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats.Exported != 0 {
		t.Errorf("post-upgrade incremental run exported %d, want 0 (the migration did not match the fresh token)", res.Stats.Exported)
	}
	if res.Stats.SkippedManifest == 0 {
		t.Error("post-upgrade run skipped nothing; the re-scoped keys did not match the re-run's keys")
	}
	if got := countTree(t, out, ".html"); got != htmlBefore {
		t.Errorf("html count changed across the upgrade: %d then %d (duplicate files written)", htmlBefore, got)
	}

	m, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != 6 {
		t.Errorf("manifest version after upgrade = %d, want 6", m.Version)
	}
	if m.Rekeyed != 0 {
		t.Errorf("a second load re-scoped %d entries, want 0 (migration is not idempotent)", m.Rekeyed)
	}
	if m.Len() == 0 {
		t.Fatal("manifest empty after upgrade")
	}
	for k := range m.Entries {
		if strings.Count(k, "\x00") != 2 {
			t.Errorf("key %q is not store-qualified after upgrade", k)
		}
	}
}

// covers: MA-134, R5, S30
// The one-time upgrade is visible in the run log: the manifest re-scope count
// and the index repair line both appear, so the operator sees the cost and can
// tie the first-run duration to it. The manifest re-scope (cause) is logged
// BEFORE the index-key migration (effect) — matching reindex and the README
// sample (friction #17) — and the index line carries the "(one-time upgrade)"
// reassurance.
func TestUpgradeIsLogged(t *testing.T) {
	out, _, src := buildThenDowngrade(t)
	var buf bytes.Buffer
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true}, log.New(&buf, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	logs := buf.String()
	reScope := strings.Index(logs, "re-scoped 2 manifest entries by store")
	indexKeys := strings.Index(logs, "migrating index keys (2 rows)")
	if reScope < 0 {
		t.Errorf("run log missing the manifest re-scope line:\n%s", logs)
	}
	if indexKeys < 0 {
		t.Errorf("run log missing the index migration line:\n%s", logs)
	}
	if reScope >= 0 && indexKeys >= 0 && reScope > indexKeys {
		t.Errorf("cause/effect log order reversed: index-key migration is logged before the manifest re-scope:\n%s", logs)
	}
	if !strings.Contains(logs, "migrating index keys (2 rows) (one-time upgrade)") {
		t.Errorf("index migration line lacks the \"(one-time upgrade)\" reassurance:\n%s", logs)
	}
}
