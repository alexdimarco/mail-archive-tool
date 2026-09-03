package graph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// covers: MA-98, R17, S29
// An unattended Graph run must never hang while holding the archive lock: a
// server that goes silent after accepting the request (no headers, or a body
// that never ends) is cut off within the configured deadlines, both for JSON
// listings and for a MIME download. The positive twin — a prompt server
// succeeds — comes first.
func TestGraphRequestsAreBounded(t *testing.T) {
	stallHeaders := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"t","token_type":"Bearer","expires_in":3600}`))
	})
	mux.HandleFunc("/users/u/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"value":[{"id":"F","displayName":"Inbox","childFolderCount":0}]}`))
	})
	mux.HandleFunc("/users/slow/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		<-stallHeaders // never answers until the test ends
	})
	mux.HandleFunc("/users/u/messages/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("From: a@x\r\n\r\npartial"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-stallHeaders // body never completes
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer close(stallHeaders)

	c := New(context.Background(), Config{Tenant: "t", ClientID: "c", ClientSecret: "s", BaseURL: srv.URL, TokenURL: srv.URL + "/token",
		RequestTimeout: 300 * time.Millisecond, MIMETimeout: 300 * time.Millisecond})

	folders, err := c.Folders(context.Background(), "u")
	if err != nil || len(folders) != 1 {
		t.Fatalf("prompt server: folders=%v err=%v", folders, err)
	}

	start := time.Now()
	_, err = c.Folders(context.Background(), "slow")
	if err == nil {
		t.Fatal("a stalled listing did not fail")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("stalled listing took %v; the deadline did not cut it off", d)
	}

	start = time.Now()
	_, err = c.MIME(context.Background(), "u", "M1")
	if err == nil {
		t.Fatal("a stalled download did not fail")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("stalled download took %v; the deadline did not cut it off", d)
	}
	if !strings.Contains(err.Error(), "M1") {
		t.Errorf("download error does not name the message: %v", err)
	}
}
