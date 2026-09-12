package app

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// twoMailboxServer serves two mailboxes u1 and u2, each with ONE Inbox message
// that shares the SAME Internet-Message-ID <shared@x> (distinct Graph ids).
func twoMailboxServer() *httptest.Server {
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(s))
	}
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
	})
	for _, u := range []string{"u1", "u2"} {
		id := strings.ToUpper(u) + "M"
		mux.HandleFunc("/users/"+u+"/mailFolders", func(w http.ResponseWriter, r *http.Request) {
			j(w, `{"value":[{"id":"F_IN","displayName":"Inbox","childFolderCount":0}]}`)
		})
		mux.HandleFunc("/users/"+u+"/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
			j(w, `{"value":[{"id":"`+id+`","internetMessageId":"<shared@x>","subject":"shared","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-01T09:00:00Z"}]}`)
		})
		mux.HandleFunc("/users/"+u+"/messages/", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("From: a@example.com\r\nSubject: shared\r\nMessage-ID: <shared@x>\r\nDate: Sat, 01 Mar 2025 09:00:00 +0000\r\n\r\nbody\r\n"))
		})
	}
	return httptest.NewServer(mux)
}

// covers: MA-230, R1, R6, S39
// Membership dedup is MAILBOX-scoped, not archive-wide. Two mailboxes archived
// into one -out that happen to share an Internet-Message-ID must EACH keep their
// own physical copy (R6): the second mailbox's message must not be skipped onto
// the first's record (which would drop it from the second and corrupt the first's
// timeline — R1/R21). KeysForIdentity is archive-wide, so the fast-path filters to
// the current token before deciding to skip.
func TestMembershipSkipIsMailboxScoped(t *testing.T) {
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)
	srv := twoMailboxServer()
	defer srv.Close()

	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1", "u2"},
		BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	if _, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Index: false}, logger); err != nil {
		t.Fatalf("run: %v", err)
	}

	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	tok1 := m.Token(state.MailboxSourceID("u1"), "u1")
	tok2 := m.Token(state.MailboxSourceID("u2"), "u2")
	refs := m.KeysForIdentity("mid:shared@x")
	if len(refs) != 2 {
		t.Fatalf("shared Message-ID has %d records, want 2 (one per mailbox) — the second mailbox's copy was dropped by an archive-wide skip (R1/R6)", len(refs))
	}
	seen := map[string]bool{}
	for _, r := range refs {
		if strings.HasPrefix(r.Key, tok1+"\x00") {
			seen["u1"] = true
		}
		if strings.HasPrefix(r.Key, tok2+"\x00") {
			seen["u2"] = true
		}
	}
	if !seen["u1"] || !seen["u2"] {
		t.Errorf("both mailboxes must own a copy of the shared Message-ID: got u1=%v u2=%v", seen["u1"], seen["u2"])
	}
}
