package graph

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// within runs fn and fails the test when it has not returned within d: the time
// guard for a loop that, unguarded, would never end.
func within(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not return within %v: the loop is not bounded", what, d)
	}
}

func tokenHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
}

// covers: MA-314, R10, R17
// A Graph listing that repeats a nextLink stops in every paging loop with a
// typed *ListingError, never a quiet end of listing: in Folders (its first page
// names itself as the next one) the call fails, and in Messages (page 2 names
// itself after page 1) the call fails after handing each message to the
// callback once. The stop names itself, never the raw link.
func TestNextLinkRepeatStopsEveryLoop(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc("/users/u/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"value":[{"id":"F_IN","displayName":"Inbox","childFolderCount":0}],
			"@odata.nextLink":"`+baseOf(r)+r.URL.RequestURI()+`"}`)
	})
	mux.HandleFunc("/users/u/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
		id := "M2"
		if r.URL.Query().Get("page") == "" {
			id = "M1"
		}
		writeJSON(w, `{"value":[{"id":"`+id+`","internetMessageId":"<`+id+`@x>","receivedDateTime":"2025-03-01T09:00:00Z"}],
			"@odata.nextLink":"`+baseOf(r)+`/users/u/mailFolders/F_IN/messages?page=2"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)
	ctx := context.Background()

	var ferr error
	within(t, 2*time.Second, "Folders over a repeating nextLink", func() {
		_, ferr = c.Folders(ctx, "u", FolderFilter{IncludeDeleted: true, IncludeJunk: true})
	})
	var le *ListingError
	if !errors.As(ferr, &le) || le.Kind != ListingRepeat {
		t.Fatalf("a repeating nextLink in Folders is not a typed repeat stop: %v", ferr)
	}

	var seen []string
	var merr error
	within(t, 2*time.Second, "Messages over a repeating nextLink", func() {
		merr = c.Messages(ctx, "u", "F_IN", func(m MessageRef) error {
			seen = append(seen, m.ID)
			return nil
		})
	})
	if !errors.As(merr, &le) || le.Kind != ListingRepeat {
		t.Fatalf("a repeating nextLink in Messages is not a typed repeat stop (a nil here is a quiet end of listing): %v", merr)
	}
	if !reflect.DeepEqual(seen, []string{"M1", "M2"}) {
		t.Errorf("messages handed to the callback = %v, want [M1 M2]", seen)
	}
	if strings.Contains(merr.Error(), "page=2") || strings.Contains(ferr.Error(), "$top") {
		t.Errorf("a guard stop quotes the raw link: %q / %q", merr, ferr)
	}
}

// covers: MA-314, R10, R17
// The guard's page cap and its scope: a listing that serves fresh nextLinks for
// ever is stopped after exactly the cap's pages (scaled to 5) with a typed stop
// naming the cap, and a second listing on the same client gets its own 5 pages
// (each loop counts its own). The repeat set refuses a link its loop fetched.
// The host/path pin, which the extraction workspace's loops turn on from its E1
// (capture's waits for step SGc), refuses a link whose scheme, host, port or
// path leaves the base, after XC25's normalization (case-insensitive host,
// default port, a path prefix on a segment boundary), and names the host.
func TestNextLinkGuardPageCapAndPin(t *testing.T) {
	var mu sync.Mutex
	pages := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc("/users/u/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		pages++
		n := pages
		mu.Unlock()
		writeJSON(w, `{"value":[],"@odata.nextLink":"`+baseOf(r)+`/users/u/mailFolders/F_IN/messages?page=`+strconv.Itoa(n+1)+`"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := boundedClient(srv, Config{PageCap: 5})
	ctx := context.Background()

	for run := 1; run <= 2; run++ {
		var err error
		within(t, 2*time.Second, "Messages over endless fresh nextLinks", func() {
			err = c.Messages(ctx, "u", "F_IN", func(MessageRef) error { return nil })
		})
		var le *ListingError
		if !errors.As(err, &le) || le.Kind != ListingPageCap || le.Limit != 5 {
			t.Fatalf("listing %d: endless fresh nextLinks are not a typed page-cap stop at 5: %v", run, err)
		}
		if !strings.Contains(err.Error(), "5") {
			t.Errorf("the page-cap stop does not name the cap: %v", err)
		}
		mu.Lock()
		got := pages
		mu.Unlock()
		if got != 5*run {
			t.Errorf("after listing %d the server served %d pages, want %d (5 per loop)", run, got, 5*run)
		}
	}

	g := c.newPageGuard(false)
	if err := g.admit(srv.URL + "/a"); err != nil {
		t.Fatalf("a first link was refused: %v", err)
	}
	var le *ListingError
	if err := g.admit(srv.URL + "/a"); !errors.As(err, &le) || le.Kind != ListingRepeat {
		t.Errorf("a link the loop already fetched is not a repeat stop: %v", err)
	}

	pc := New(ctx, Config{Tenant: "t", ClientID: "c", ClientSecret: "s",
		BaseURL: "https://graph.example.test/v1.0", TokenURL: srv.URL + "/token"})
	links := []struct {
		link, host string
		ok         bool
	}{
		{"https://graph.example.test/v1.0/users/u/mailFolders?$skiptoken=X1", "", true},
		{"https://GRAPH.example.test:443/v1.0/me/messages?$skip=10", "", true},
		{"https://graph.example.test/v1.0", "", true},
		{"https://evil.example/v1.0/users/u/mailFolders", "evil.example", false},
		{"http://graph.example.test/v1.0/users/u/mailFolders", "graph.example.test", false},
		{"https://graph.example.test:8443/v1.0/users/u", "graph.example.test", false},
		{"https://graph.example.test/beta/users/u", "graph.example.test", false},
		{"https://graph.example.test/v1.0x/users/u", "graph.example.test", false},
		{"https://graph.example.test/v1.0/../beta/users/u", "graph.example.test", false},
		{"https://graph.example.test.evil.example/v1.0/users/u", "graph.example.test.evil.example", false},
	}
	for _, l := range links {
		err := pc.newPageGuard(true).admit(l.link)
		if l.ok {
			if err != nil {
				t.Errorf("the pin refused a link on the base: %s: %v", l.link, err)
			}
			continue
		}
		if !errors.As(err, &le) || le.Kind != ListingForeignHost || le.Host != l.host {
			t.Errorf("the pin did not refuse %s naming %q: %v", l.link, l.host, err)
			continue
		}
		if !strings.Contains(err.Error(), l.host) {
			t.Errorf("the foreign-host stop does not name the host: %v", err)
		}
	}
}

// covers: MA-314, R10, R17
// MA-314's folder-list-failed gap case: a repeating nextLink in Messages ends
// that folder as a folder-list-failed gap, and the walk goes on.
func TestRepeatingNextLinkIsAFolderListFailedGap(t *testing.T) {
	t.Skip("MA-314: the folder-list-failed gap case needs the S1a/S3 capture-gap ledger; until fault-isolation S3 a Messages stop fails the mailbox through today's error path (MA-540); lands in FI-S3")
}

// covers: MA-314, R10, R17
// MA-314's capture foreign-host case: a nextLink in capture's Folders or Messages
// that leaves the base host is refused, no request goes there and no bearer
// token reaches it.
func TestCaptureNextLinkForeignHostRefused(t *testing.T) {
	t.Skip("MA-314: capture's Folders and Messages carry no host/path pin until fault-isolation step FI-SGc, which waits for MA-443's recorded nextLink shapes (XC25's gate; lifting it is the owner's ESC-7); a foreign nextLink in capture's listings is an open gap, not accepted")
}

// covers: MA-541, R10, R17
// Folders keeps one visited folder-id set across the call and a depth cap of
// 300: a cycle ends, a moved folder is listed once, and a level past the cap
// fails with a typed error while a chain exactly at the cap completes.
func TestFoldersHierarchyIsBounded(t *testing.T) {
	t.Skip("MA-541: Folders has no visited folder-id set and no depth cap yet; lands in FI-SG")
}
