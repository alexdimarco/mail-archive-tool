package app

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"mail-archive-tool/internal/export"
)

// physGraphServer serves u1/Inbox with two distinct-Message-ID messages and, when
// *honor is set AND the client asked for immutable ids, emits Preference-Applied so
// graph.Messages sets ref.PhysID = the message id. It records $value fetches so a
// test can prove no re-download. Flipping *honor between runs models a tenant that
// begins honoring immutable ids (the first v6 walk of an upgraded archive).
func physGraphServer(honor *bool) (func() map[string]int, *httptest.Server) {
	var mu sync.Mutex
	hits := map[string]int{}
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(s))
	}
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/users/u1/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"value":[{"id":"F_IN","displayName":"Inbox","childFolderCount":0}]}`)
	})
	mux.HandleFunc("/users/u1/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
		if *honor && strings.Contains(strings.ToLower(r.Header.Get("Prefer")), "immutableid") {
			w.Header().Set("Preference-Applied", `IdType="ImmutableId"`)
		}
		j(w, `{"value":[
			{"id":"M1","internetMessageId":"<m1@x>","subject":"subj-M1","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-01T09:00:00Z"},
			{"id":"M2","internetMessageId":"<m2@x>","subject":"subj-M2","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-02T09:00:00Z"}]}`)
	})
	mux.HandleFunc("/users/u1/messages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/users/u1/messages/"), "/$value")
		mu.Lock()
		hits[id]++
		mu.Unlock()
		w.Write([]byte("From: a@example.com\r\nSubject: subj-" + id +
			"\r\nMessage-ID: <" + strings.ToLower(id) + "@x>\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\nbody\r\n"))
	})
	snap := func() map[string]int {
		mu.Lock()
		defer mu.Unlock()
		out := map[string]int{}
		for k, v := range hits {
			out[k] = v
		}
		return out
	}
	return snap, httptest.NewServer(mux)
}

// covers: MA-246, R17, S38, S39
// The live fast-path uses the immutable id (rev-6.1 §5). Upgrading a floor archive
// to id-honoring is STORM-FREE: the first id-bearing walk BACKFILLS each lone
// sibling's PhysID FROM THE LISTING with no re-download (option D's promise holds),
// and thereafter each message is skipped by its exact immutable id. A tenant that
// withholds the id degrades to the Message-ID-membership floor. This exercises the
// skip-vs-download half; the exporter's placement/split is MA-244/245.
func TestGraphPhysIDBackfillNoRedownloadThenSkipByID(t *testing.T) {
	out := tmpDir(t)
	honor := false
	hitsOf, srv := physGraphServer(&honor)
	defer srv.Close()
	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"}, BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	opts := Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}
	logger := log.New(io.Discard, "", 0)
	ctx := context.Background()

	// Run 1 — the tenant WITHHOLDS the id (floor): capture both, PhysID unset.
	if _, err := RunGraph(ctx, g, opts, logger); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if h := hitsOf(); h["M1"] != 1 || h["M2"] != 1 {
		t.Fatalf("run 1 hits = %v, want each message fetched once", h)
	}
	man := loadManifest(t, out)
	for _, id := range []string{"mid:m1@x", "mid:m2@x"} {
		refs := man.KeysForIdentity(id)
		if len(refs) != 1 || refs[0].PhysID != "" {
			t.Fatalf("after floor run: %s PhysID = %+v, want empty", id, refs)
		}
	}

	// Run 2 — the tenant NOW honors immutable ids. Each lone sibling is backfilled
	// FROM THE LISTING: skipped, no re-download, and the id is now stored.
	honor = true
	r2, err := RunGraph(ctx, g, opts, logger)
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if r2.Stats.SkippedManifest != 2 || r2.Stats.Exported != 0 {
		t.Errorf("run 2 skipped=%d exported=%d, want 2/0", r2.Stats.SkippedManifest, r2.Stats.Exported)
	}
	if h := hitsOf(); h["M1"] != 1 || h["M2"] != 1 {
		t.Errorf("run 2 RE-DOWNLOADED on upgrade (hits=%v, want each still 1) — the backfill-from-listing storm-free path failed", h)
	}
	man = loadManifest(t, out)
	want := map[string]string{"mid:m1@x": "M1", "mid:m2@x": "M2"}
	for id, wid := range want {
		refs := man.KeysForIdentity(id)
		if len(refs) != 1 || refs[0].PhysID != wid {
			t.Errorf("after upgrade: %s PhysID = %+v, want %s backfilled from the listing", id, refs, wid)
		}
	}

	// Run 3 — steady state: skip by the matched immutable id, still no download.
	r3, err := RunGraph(ctx, g, opts, logger)
	if err != nil {
		t.Fatalf("run 3: %v", err)
	}
	if r3.Stats.SkippedManifest != 2 {
		t.Errorf("run 3 skipped=%d, want 2 (skip by matched id)", r3.Stats.SkippedManifest)
	}
	if h := hitsOf(); h["M1"] != 1 || h["M2"] != 1 {
		t.Errorf("run 3 re-downloaded (hits=%v) — skip-by-matched-id failed", h)
	}
}
