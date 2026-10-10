package graph

import (
	"context"
	"errors"
	"io"
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

// roundTripFunc is an http.RoundTripper made of a function (fault injection).
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// covers: MA-314, R10, R17
// MA-314's capture foreign-host case: a nextLink in capture's Folders or Messages
// that leaves the base host is refused with a typed stop naming that host, no
// request goes there and no bearer token reaches it. The foreign host is a name
// the client's transport answers itself (WrapTransport), recording what reached
// it, so nothing leaves the process. A skip encoder until step SGc turns the pin
// on in capture's loops: run without the skip today, the token reaches the
// foreign host from both loops.
func TestCaptureNextLinkForeignHostRefused(t *testing.T) {
	t.Skip("MA-314: capture's Folders and Messages carry no host/path pin until fault-isolation step FI-SGc, which waits for MA-443's recorded nextLink shapes (XC25's gate; lifting it is the owner's ESC-7); a foreign nextLink in capture's listings is an open gap, not accepted")
	const foreign = "foreign.example.test"
	var mu sync.Mutex
	var reached []string // the Authorization header of each request sent to the foreign host
	trap := func(rt http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Hostname() != foreign {
				return rt.RoundTrip(r)
			}
			mu.Lock()
			reached = append(reached, r.Header.Get("Authorization"))
			mu.Unlock()
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"value":[]}`)), Request: r}, nil
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc("/users/u/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"value":[{"id":"F_IN","displayName":"Inbox","childFolderCount":0}],
			"@odata.nextLink":"http://`+foreign+`/users/u/mailFolders?page=2"}`)
	})
	mux.HandleFunc("/users/u/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"value":[{"id":"M1","internetMessageId":"<m1@x>","receivedDateTime":"2025-03-01T09:00:00Z"}],
			"@odata.nextLink":"http://`+foreign+`/users/u/mailFolders/F_IN/messages?page=2"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := boundedClient(srv, Config{WrapTransport: trap})
	ctx := context.Background()

	_, ferr := c.Folders(ctx, "u", FolderFilter{IncludeDeleted: true, IncludeJunk: true})
	merr := c.Messages(ctx, "u", "F_IN", func(MessageRef) error { return nil })
	for _, l := range []struct {
		loop string
		err  error
	}{{"Folders", ferr}, {"Messages", merr}} {
		var le *ListingError
		if !errors.As(l.err, &le) || le.Kind != ListingForeignHost || le.Host != foreign {
			t.Errorf("%s: a nextLink to %s is not a typed foreign-host stop naming it: %v", l.loop, foreign, l.err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reached) != 0 {
		t.Errorf("%d request(s) went to the foreign host %s, with Authorization %q: the bearer token left Microsoft Graph", len(reached), foreign, reached)
	}
}

// hierarchyServer serves mailbox u's folders: children(parent) lists a folder's
// child ids ("" is the top level), each folder named by its id and claiming
// children exactly when it has some. It counts the childFolders requests made
// for each parent.
func hierarchyServer(t *testing.T, children func(parent string) []string) (*httptest.Server, func(parent string) int) {
	t.Helper()
	var mu sync.Mutex
	expanded := map[string]int{}
	list := func(w http.ResponseWriter, parent string) {
		var vals []string
		for _, id := range children(parent) {
			n := 0
			if len(children(id)) > 0 {
				n = 1
			}
			vals = append(vals, `{"id":"`+id+`","displayName":"`+id+`","childFolderCount":`+strconv.Itoa(n)+`}`)
		}
		writeJSON(w, `{"value":[`+strings.Join(vals, ",")+`]}`)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc("/users/u/mailFolders", func(w http.ResponseWriter, r *http.Request) { list(w, "") })
	mux.HandleFunc("/users/u/mailFolders/", func(w http.ResponseWriter, r *http.Request) {
		id, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/users/u/mailFolders/"), "/childFolders")
		if !ok {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		expanded[id]++
		mu.Unlock()
		list(w, id)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func(parent string) int {
		mu.Lock()
		defer mu.Unlock()
		return expanded[parent]
	}
}

// chain is a hierarchy of n levels, L1 at the top, each level the only child of
// the one above it.
func chain(n int) func(string) []string {
	return func(parent string) []string {
		k := 0
		if parent != "" {
			k, _ = strconv.Atoi(strings.TrimPrefix(parent, "L"))
		}
		if k >= n {
			return nil
		}
		return []string{"L" + strconv.Itoa(k+1)}
	}
}

// folderIDs returns the walk's folder ids in order and fails on a duplicate.
func folderIDs(t *testing.T, fs []Folder) []string {
	t.Helper()
	seen := map[string]bool{}
	var ids []string
	for _, f := range fs {
		if seen[f.ID] {
			t.Errorf("folder %s listed twice", f.ID)
		}
		seen[f.ID] = true
		ids = append(ids, f.ID)
	}
	return ids
}

// covers: MA-541, R10, R17
// Folders keeps one visited folder-id set across the call and a depth cap of
// 300 levels, the Graph twin of MA-317. A childFolders response that lists an
// ancestor (A → B → A) returns under a 1 s guard with A and B each listed once
// and no second childFolders request for A. A folder listed under two parents
// (moved during the walk) is listed once and the walk completes. A chain of
// fresh folders 301 levels deep fails with the typed depth error naming the
// cap, under a time guard. Twins: a healthy three-level tree, and a chain of
// exactly 300 levels (counted as the confirmation counts them: /mailFolders is
// level 1), each listed once with no error.
func TestFoldersHierarchyIsBounded(t *testing.T) {
	ctx := context.Background()
	all := FolderFilter{IncludeDeleted: true, IncludeJunk: true}
	from := func(tree map[string][]string) func(string) []string {
		return func(parent string) []string { return tree[parent] }
	}

	t.Run("healthy three-level tree", func(t *testing.T) {
		srv, _ := hierarchyServer(t, from(map[string][]string{"": {"A", "B"}, "A": {"A1"}, "A1": {"A1a"}}))
		fs, err := newTestClient(srv).Folders(ctx, "u", all)
		if err != nil {
			t.Fatal(err)
		}
		if ids := folderIDs(t, fs); !reflect.DeepEqual(ids, []string{"A", "A1", "A1a", "B"}) {
			t.Errorf("folders = %v, want [A A1 A1a B]", ids)
		}
	})

	t.Run("a cycle ends", func(t *testing.T) {
		srv, expanded := hierarchyServer(t, from(map[string][]string{"": {"A"}, "A": {"B"}, "B": {"A"}}))
		var fs []Folder
		var err error
		within(t, time.Second, "Folders over a cyclic childFolders response", func() {
			fs, err = newTestClient(srv).Folders(ctx, "u", all)
		})
		if err != nil {
			t.Fatalf("a revisited folder failed the walk: %v", err)
		}
		if ids := folderIDs(t, fs); !reflect.DeepEqual(ids, []string{"A", "B"}) {
			t.Errorf("folders = %v, want [A B]", ids)
		}
		if n := expanded("A"); n != 1 {
			t.Errorf("A's childFolders were requested %d times, want 1", n)
		}
	})

	t.Run("a moved folder is listed once", func(t *testing.T) {
		srv, _ := hierarchyServer(t, from(map[string][]string{"": {"P1", "P2"}, "P1": {"X"}, "P2": {"X"}}))
		fs, err := newTestClient(srv).Folders(ctx, "u", all)
		if err != nil {
			t.Fatalf("a folder listed under two parents failed the walk: %v", err)
		}
		if ids := folderIDs(t, fs); !reflect.DeepEqual(ids, []string{"P1", "X", "P2"}) {
			t.Errorf("folders = %v, want [P1 X P2]", ids)
		}
	})

	t.Run("a chain at the cap completes", func(t *testing.T) {
		srv, _ := hierarchyServer(t, chain(maxFolderDepth))
		var fs []Folder
		var err error
		within(t, 10*time.Second, "Folders over a 300-level chain", func() {
			fs, err = newTestClient(srv).Folders(ctx, "u", all)
		})
		if err != nil {
			t.Fatalf("a chain exactly at the %d-level cap failed: %v", maxFolderDepth, err)
		}
		if ids := folderIDs(t, fs); len(ids) != maxFolderDepth || ids[len(ids)-1] != "L300" {
			t.Errorf("the at-cap chain listed %d folders (last %v), want 300 ending at L300", len(ids), ids[len(ids)-1:])
		}
	})

	t.Run("a chain past the cap fails typed", func(t *testing.T) {
		srv, _ := hierarchyServer(t, chain(maxFolderDepth+1))
		var err error
		within(t, 10*time.Second, "Folders over a 301-level chain", func() {
			_, err = newTestClient(srv).Folders(ctx, "u", all)
		})
		var de *DepthError
		if !errors.As(err, &de) || de.Limit != 300 {
			t.Fatalf("a chain 301 levels deep is not the typed depth error at 300: %v", err)
		}
		if !strings.Contains(err.Error(), "300") {
			t.Errorf("the depth error does not name the cap: %v", err)
		}
	})
}

// covers: MA-541, R10
// MA-541's breadth case: a childFolders response that invents two fresh child
// folders at every level, inside the depth cap, is bounded in depth and per
// loop by the page cap but not in total, so a misbehaving endpoint keeps one
// walk going for as long as it answers; Folders must return under a time guard,
// with a typed stop, never a quiet end of the walk. The fake's tree is 32 levels
// deep, well inside the 300-level cap, so only a total-folder cap can end it. A
// skip encoder until such a cap lands: run without the skip today, the walk is
// still going when the guard fires.
func TestFoldersBreadthIsBounded(t *testing.T) {
	t.Skip("MA-541 (R10): no total-folder cap: Microsoft documents no per-mailbox folder count (the 2026-10-09 confirmation), so ESC-9's safe interim (owner ruling RR6-OR2) gives none; an open gap, not accepted, until a documented figure lets a cap land")
	const depth = 32
	srv, _ := hierarchyServer(t, func(parent string) []string {
		switch {
		case parent == "":
			return []string{"N0", "N1"}
		case len(parent)-1 < depth: // "N" plus one digit per level
			return []string{parent + "0", parent + "1"}
		}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel) // ends the walk once the guard has fired
	var err error
	within(t, 10*time.Second, "Folders over a childFolders response that invents two fresh folders at every level", func() {
		_, err = newTestClient(srv).Folders(ctx, "u", FolderFilter{IncludeDeleted: true, IncludeJunk: true})
	})
	if err == nil || isContextErr(err) {
		t.Errorf("a folder tree that never stops widening ended without a typed stop: %v", err)
	}
}
