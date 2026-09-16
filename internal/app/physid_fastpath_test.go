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
// test can prove no re-download.
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
// The live fast-path uses the immutable id + the recorded content hash (rev-6.1 §5,
// content-hash arbiter). A message captured under v6 stores its immutable id AND a
// body-inclusive content hash (the churn-vs-distinct arbiter, no .eml / -raw
// needed); a re-run skips it by its EXACT immutable id with no re-download. (A
// distinct reuse is downloaded and split — MA-203/MA-244; a pre-v6 record with no
// content hash stays the floor — MA-247.)
func TestGraphPhysIDSkipByMatchedIDNoRedownload(t *testing.T) {
	out := tmpDir(t)
	honor := true
	hitsOf, srv := physGraphServer(&honor)
	defer srv.Close()
	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"}, BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	opts := Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}
	logger := log.New(io.Discard, "", 0)
	ctx := context.Background()

	// Run 1 — fresh v6 capture: each message stores its immutable id AND a content hash.
	if _, err := RunGraph(ctx, g, opts, logger); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if h := hitsOf(); h["M1"] != 1 || h["M2"] != 1 {
		t.Fatalf("run 1 hits = %v, want each fetched once", h)
	}
	man := loadManifest(t, out)
	for id, wid := range map[string]string{"mid:m1@x": "M1", "mid:m2@x": "M2"} {
		refs := man.KeysForIdentity(id)
		if len(refs) != 1 || refs[0].PhysID != wid {
			t.Fatalf("%s PhysID = %+v, want %s stored at capture", id, refs, wid)
		}
		rec, _ := man.Get(refs[0].Key)
		if rec.ContentHash == "" {
			t.Errorf("%s has no ContentHash stored — the closure's arbiter signal is missing", id)
		}
	}

	// Run 2 — steady state: skip by the matched immutable id, no re-download.
	r2, err := RunGraph(ctx, g, opts, logger)
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if r2.Stats.SkippedManifest != 2 || r2.Stats.Exported != 0 {
		t.Errorf("run 2 skipped=%d exported=%d, want 2/0 (skip by matched id)", r2.Stats.SkippedManifest, r2.Stats.Exported)
	}
	if h := hitsOf(); h["M1"] != 1 || h["M2"] != 1 {
		t.Errorf("run 2 re-downloaded (hits=%v) — skip-by-matched-id failed", h)
	}
}

// copyGraphServer serves ONE message that exists as content-identical COPIES in two
// folders (Inbox id CA, Saved id CB) — same Internet-Message-ID <c@x>, distinct
// immutable ids, identical MIME — honoring immutable ids when *honor is set.
func copyGraphServer(honor *bool) (func() map[string]int, *httptest.Server) {
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
		j(w, `{"value":[{"id":"F_IN","displayName":"Inbox","childFolderCount":0},{"id":"F_SA","displayName":"Saved","childFolderCount":0}]}`)
	})
	listing := func(id string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if *honor && strings.Contains(strings.ToLower(r.Header.Get("Prefer")), "immutableid") {
				w.Header().Set("Preference-Applied", `IdType="ImmutableId"`)
			}
			j(w, `{"value":[{"id":"`+id+`","internetMessageId":"<c@x>","subject":"Notice","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-03T09:00:00Z"}]}`)
		}
	}
	mux.HandleFunc("/users/u1/mailFolders/F_IN/messages", listing("CA"))
	mux.HandleFunc("/users/u1/mailFolders/F_SA/messages", listing("CB"))
	mux.HandleFunc("/users/u1/messages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/users/u1/messages/"), "/$value")
		mu.Lock()
		hits[id]++
		mu.Unlock()
		// IDENTICAL content for both copies (keyed by Message-ID, not id).
		w.Write([]byte("From: a@example.com\r\nSubject: Notice\r\nMessage-ID: <c@x>\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\nsame body\r\n"))
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

// covers: MA-249, R17, S38, S39
// A message COPIED into two folders (same Internet-Message-ID, identical content,
// DISTINCT Graph immutable ids) collapses to ONE mailbox-wide record whose
// content-equal id SET holds both ids (rev-6.2). The first run downloads each copy
// once (a fresh capture + one content-compare) and adds both ids; every LATER run
// skips BOTH by set membership — no re-download, no id flap. Without the set (a
// single-id record) the copy whose id is not stored re-downloads on every run
// forever.
func TestGraphCopiedMessageNoRedownloadNoFlap(t *testing.T) {
	out := tmpDir(t)
	honor := true
	hitsOf, srv := copyGraphServer(&honor)
	defer srv.Close()
	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"}, BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	opts := Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}
	logger := log.New(io.Discard, "", 0)
	ctx := context.Background()

	if _, err := RunGraph(ctx, g, opts, logger); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	man := loadManifest(t, out)
	refs := man.KeysForIdentity("mid:c@x")
	if len(refs) != 1 {
		t.Fatalf("copied message has %d records, want 1 (one mailbox-wide copy)", len(refs))
	}
	base := refs[0].Key
	if !man.HasPhysID(base, "CA") || !man.HasPhysID(base, "CB") {
		t.Errorf("record id-set missing a copy: HasPhysID CA=%v CB=%v, want both", man.HasPhysID(base, "CA"), man.HasPhysID(base, "CB"))
	}

	// Every later run skips BOTH copies with no re-download.
	before := hitsOf()
	r2, err := RunGraph(ctx, g, opts, logger)
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if r2.Stats.SkippedManifest != 2 || r2.Stats.Exported != 0 {
		t.Errorf("run 2 skipped=%d exported=%d, want 2/0 (both copies skip by set membership)", r2.Stats.SkippedManifest, r2.Stats.Exported)
	}
	if h := hitsOf(); h["CA"] != before["CA"] || h["CB"] != before["CB"] {
		t.Errorf("a copied message was re-downloaded on run 2 (before=%v after=%v) — the id-set skip failed", before, h)
	}
}
