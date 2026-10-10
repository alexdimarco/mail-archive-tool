package graph

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// MessagePage fake modes: how the page endpoint answers.
const (
	modeNormal       = iota
	modeRejectExpand // a 400 for any request that carries $expand, as a tenant that does not support it
	modeUnauthorized // a 401 for everything
)

// pageFake serves /me/mailFolders/F_IN/messages: page 1 holds M1..M4 with their
// extended properties in different spellings and orders, and a nextLink whose
// page 2 holds M5.
type pageFake struct {
	rec   recorder
	mode  atomic.Int32
	honor atomic.Bool // answer Preference-Applied: IdType="ImmutableId"
	srv   *httptest.Server
}

func pageEnvelope(id string) string {
	return `"id":"` + id + `","internetMessageId":"<` + strings.ToLower(id) + `@x>","subject":"subj-` + id + `",` +
		`"from":{"emailAddress":{"name":"Alice <A>","address":"alice@x.example"}},` +
		`"toRecipients":[{"emailAddress":{"address":"bob@x.example"}}],"ccRecipients":[{"emailAddress":{"address":"carol@x.example"}}],` +
		`"receivedDateTime":"2025-03-02T09:00:00Z","hasAttachments":true,"importance":"high","isRead":false,` +
		`"categories":["Red"],"parentFolderId":"F_IN"`
}

func newPageFake(t *testing.T) *pageFake {
	t.Helper()
	f := &pageFake{}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc("/me/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
		f.rec.add(r)
		expanded := r.URL.Query().Get("$expand") != ""
		switch f.mode.Load() {
		case modeUnauthorized:
			http.Error(w, `{"error":{"code":"InvalidAuthenticationToken","message":"expired"}}`, http.StatusUnauthorized)
			return
		case modeRejectExpand:
			if expanded {
				http.Error(w, `{"error":{"code":"ErrorInvalidProperty","message":"The expand expression is not supported."}}`, http.StatusBadRequest)
				return
			}
		}
		if f.honor.Load() {
			w.Header().Set("Preference-Applied", `IdType="ImmutableId"`)
		}
		ext := func(props string) string {
			if !expanded {
				return ""
			}
			return `,"singleValueExtendedProperties":[` + props + `]`
		}
		if r.URL.Query().Get("$skiptoken") == "P2" {
			writeJSON(w, `{"value":[{`+pageEnvelope("M5")+ext(`{"id":"Integer 0x0E08","value":"9"}`)+`}]}`)
			return
		}
		vals := []string{
			// size first, then sensitivity; ids spelled the way the tenant likes
			`{` + pageEnvelope("M1") + ext(`{"id":"Integer 0xe08","value":"12345"},{"id":"Integer 0x36","value":"2"}`) + `}`,
			// the other way round: a positional decode would read 3 bytes of size as sensitivity "confidential"
			`{` + pageEnvelope("M2") + ext(`{"id":"Integer 0x36","value":"3"},{"id":"Integer 0xe08","value":"7"}`) + `}`,
			// a size of 2 alone is not a sensitivity of "private"
			`{` + pageEnvelope("M3") + ext(`{"id":"Integer 0xE08","value":"2"}`) + `}`,
			`{` + pageEnvelope("M4") + `}`,
		}
		// as Graph does, the link carries the first request's query (the $expand too)
		writeJSON(w, `{"value":[`+strings.Join(vals, ",")+`],"@odata.nextLink":"`+baseOf(r)+`/me/mailFolders/F_IN/messages?$skiptoken=P2&`+r.URL.RawQuery+`"}`)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// wantRef is the MessageRef the fake's envelope decodes to.
func wantRef(id string, size int64, sens, phys string) MessageRef {
	notRead := false
	return MessageRef{
		ID: id, InternetMessageID: strings.ToLower(id) + "@x", Received: time.Date(2025, 3, 2, 9, 0, 0, 0, time.UTC),
		PhysID: phys, Subject: "subj-" + id, From: "alice@x.example", To: []string{"bob@x.example"}, Cc: []string{"carol@x.example"},
		HasAttachments: true, Importance: "high", IsRead: &notRead, Categories: []string{"Red"}, Sensitivity: sens,
		FromName: "Alice <A>", ParentFolderID: "F_IN", SizeHint: size,
	}
}

// covers: MA-401, R17, R29, S52, S20
// MessagePage returns one page of a folder, newest first, with the envelope,
// the sender's name and address, the folder id and the size hint. Its first
// request carries capture's $select plus parentFolderId, $orderby
// receivedDateTime desc, the requested $top, and one best-effort $expand of the
// size and sensitivity extended properties with an id-aware `or` filter. Those
// are decoded by property id, never by position: the reversed order, a different
// spelling of the id, and a size of 2 that must not read as a sensitivity. The
// next page is reached by the returned nextLink as given. The immutable-id
// "honored" flag is the first page's Preference-Applied (PhysID = ID), and a
// page reached by nextLink takes it from the caller's recorded flag. A tenant
// that rejects the $expand gets the first page again without it (the MA-285
// fallback: sizes unknown, sensitivity ""), but a 401 is not retried.
func TestMessagePageContract(t *testing.T) {
	f := newPageFake(t)
	c := meClient(t, f.srv, Config{}, time.Now().Add(time.Hour))
	ctx := context.Background()

	// not honored
	refs, next, err := c.MessagePage(ctx, "ignored", "F_IN", "", PageOpts{Top: 100})
	if err != nil {
		t.Fatal(err)
	}
	want := []MessageRef{wantRef("M1", 12345, "private", ""), wantRef("M2", 7, "confidential", ""), wantRef("M3", 2, "", ""), wantRef("M4", -1, "", "")}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("MessagePage =\n %+v\nwant\n %+v", refs, want)
	}
	if !strings.Contains(next, "/me/mailFolders/F_IN/messages?$skiptoken=P2&") {
		t.Fatalf("next = %q", next)
	}
	firstNext := next
	q := f.rec.all()[0]
	f.rec.onlyMeGETs(t, "/me/mailFolders/F_IN/messages")
	if got, want := q.URL.Query().Get("$select"), strings.Join(messageSelectFields, ",")+",parentFolderId"; got != want {
		t.Errorf("$select = %q, want %q", got, want)
	}
	if got := q.URL.Query().Get("$orderby"); got != "receivedDateTime desc" {
		t.Errorf("$orderby = %q", got)
	}
	if got := q.URL.Query().Get("$top"); got != "100" {
		t.Errorf("$top = %q", got)
	}
	wantExpand := "singleValueExtendedProperties($filter=id eq 'Integer 0x0E08' or id eq 'Integer 0x0036')"
	if got := q.URL.Query().Get("$expand"); got != wantExpand {
		t.Errorf("$expand = %q, want %q", got, wantExpand)
	}

	// the resolution page size, and its clamp
	for _, c2 := range []struct{ top, want string }{{"1000", "1000"}, {"5000", "1000"}, {"0", "100"}} {
		n := map[string]int{"1000": 1000, "5000": 5000, "0": 0}[c2.top]
		if _, _, err := c.MessagePage(ctx, "ignored", "F_IN", "", PageOpts{Top: n}); err != nil {
			t.Fatal(err)
		}
		if got := f.rec.all()[len(f.rec.all())-1].URL.Query().Get("$top"); got != c2.want {
			t.Errorf("Top %s asked $top=%s, want %s", c2.top, got, c2.want)
		}
	}

	// the next page follows the link as given; without the flag nothing is honored
	refs, next, err = c.MessagePage(ctx, "ignored", "F_IN", next, PageOpts{})
	if err != nil || next != "" || len(refs) != 1 || refs[0].ID != "M5" || refs[0].PhysID != "" || refs[0].SizeHint != 9 {
		t.Fatalf("page 2 = %+v, next %q, %v", refs, next, err)
	}
	last := f.rec.all()[len(f.rec.all())-1]
	if last.URL.Query().Get("$skiptoken") != "P2" || last.URL.Query().Get("$top") != "100" {
		t.Errorf("the link was not followed as given: %s", last.URL.RequestURI())
	}
	// a page reached by nextLink takes the recorded flag
	refs, _, err = c.MessagePage(ctx, "ignored", "F_IN", firstNext, PageOpts{Honored: true})
	if err != nil || len(refs) != 1 || refs[0].PhysID != "M5" {
		t.Fatalf("a page with the recorded honored flag: %+v, %v", refs, err)
	}

	// honored on the first page: PhysID is the id
	f.honor.Store(true)
	refs, _, err = c.MessagePage(ctx, "ignored", "F_IN", "", PageOpts{Top: 100})
	if err != nil || len(refs) != 4 || refs[0].PhysID != "M1" || refs[3].PhysID != "M4" {
		t.Fatalf("honored first page: %+v, %v", refs, err)
	}
	f.honor.Store(false)

	// a tenant that rejects the $expand: the first page again without it, once
	f.mode.Store(modeRejectExpand)
	before := len(f.rec.all())
	refs, _, err = c.MessagePage(ctx, "ignored", "F_IN", "", PageOpts{Top: 100})
	if err != nil {
		t.Fatalf("the $expand fallback: %v", err)
	}
	if want := []MessageRef{wantRef("M1", -1, "", ""), wantRef("M2", -1, "", ""), wantRef("M3", -1, "", ""), wantRef("M4", -1, "", "")}; !reflect.DeepEqual(refs, want) {
		t.Errorf("fallback page = %+v", refs)
	}
	reqs := f.rec.all()[before:]
	if len(reqs) != 2 || reqs[0].URL.Query().Get("$expand") == "" || reqs[1].URL.Query().Get("$expand") != "" ||
		reqs[1].URL.Query().Get("$orderby") != "receivedDateTime desc" || reqs[1].URL.Query().Get("$top") != "100" {
		t.Errorf("the fallback made %d request(s), want the $expand one then the plain one: %v", len(reqs), reqs)
	}

	// an expired sign-in is not retried as if the $expand were the cause
	f.mode.Store(modeUnauthorized)
	before = len(f.rec.all())
	_, _, err = c.MessagePage(ctx, "ignored", "F_IN", "", PageOpts{Top: 100})
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusUnauthorized || len(f.rec.all())-before != 1 {
		t.Errorf("a 401 is not one typed request: %v (%d requests)", err, len(f.rec.all())-before)
	}
}

// covers: MA-401, R17, R29, S52, S20
// The host/path pin is on in the workspace's loops from day one (XC25): a
// nextLink in MessagePage (as the link it is handed) and in FolderTree (as the
// page it reads says) whose scheme, host, port or path prefix leaves the base is
// refused with the typed foreign-host stop naming the host, and no request is
// made there (the transport records zero hits). The link is compared after
// normalization: the host's case and an explicit :443 are accepted, a path that is
// not under the base on a segment boundary, or that climbs out with dot segments,
// is not. A 302 from the base to another server records zero hits there. A
// caller's guard (NewPageGuard) stops a repeated link and a page past the cap.
func TestWorkspaceNextLinkPin(t *testing.T) {
	const base = "https://graph.example.test/v1.0"
	var mu sync.Mutex
	var hits []string   // host and path of every request the transport saw
	var treeLink string // the nextLink the top-level folder page carries, when set
	trap := func(http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			mu.Lock()
			hits = append(hits, strings.ToLower(r.URL.Host)+r.URL.Path)
			link := treeLink
			mu.Unlock()
			body := `{"value":[]}`
			if link != "" && r.URL.Path == "/v1.0/me/mailFolders" {
				body = `{"value":[],"@odata.nextLink":"` + link + `"}`
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})
	}
	hitCount := func() int { mu.Lock(); defer mu.Unlock(); return len(hits) }
	hitsFrom := func(n int) []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), hits[n:]...) }
	idle := httptest.NewServer(http.NotFoundHandler()) // the token endpoint the saved sign-in never reaches
	defer idle.Close()
	c := meClient(t, idle, Config{WrapTransport: trap, BaseURL: base, PageCap: 3}, time.Now().Add(time.Hour))
	ctx := context.Background()

	ok := []string{
		base + "/me/mailFolders/F_IN/messages?$skiptoken=1",
		"https://GRAPH.example.test:443/v1.0/me/mailFolders/F_IN/messages?$skiptoken=1", // host case and an explicit default port
	}
	refused := []struct{ link, host string }{
		{"https://evil.example/v1.0/me/mailFolders/F_IN/messages?x=1", "evil.example"},
		{"http://graph.example.test/v1.0/me/mailFolders/F_IN/messages", "graph.example.test"},
		{"https://graph.example.test:8443/v1.0/me/mailFolders/F_IN/messages", "graph.example.test"},
		{"https://graph.example.test/v1.0x/me/mailFolders/F_IN/messages", "graph.example.test"}, // not on a segment boundary
		{"https://graph.example.test/v1.0/../beta/me/messages", "graph.example.test"},           // dot segments climb out
		{"https://graph.example.test.evil.example/v1.0/me/messages", "graph.example.test.evil.example"},
	}
	for _, l := range ok {
		n := hitCount()
		if _, _, err := c.MessagePage(ctx, "ignored", "F_IN", l, PageOpts{}); err != nil || hitCount() != n+1 {
			t.Errorf("MessagePage refused or skipped a link on the base: %s: %v", l, err)
		}
		mu.Lock()
		treeLink = l
		mu.Unlock()
		n = hitCount()
		if _, err := c.FolderTree(ctx, "ignored", 0); err != nil || hitCount() != n+4 {
			t.Errorf("FolderTree refused or skipped a nextLink on the base: %s: %v (%d requests)", l, err, hitCount()-n)
		}
	}
	for _, l := range refused {
		n := hitCount()
		_, _, err := c.MessagePage(ctx, "ignored", "F_IN", l.link, PageOpts{})
		var le *ListingError
		if !errors.As(err, &le) || le.Kind != ListingForeignHost || le.Host != l.host {
			t.Errorf("MessagePage did not refuse %s naming %q: %v", l.link, l.host, err)
			continue
		}
		assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(l.host),
			assure.NoSideEffect(func() bool { return hitCount() == n }))

		mu.Lock()
		treeLink = l.link
		mu.Unlock()
		n = hitCount()
		if _, err := c.FolderTree(ctx, "ignored", 0); !errors.As(err, &le) || le.Kind != ListingForeignHost || le.Host != l.host {
			t.Errorf("FolderTree did not refuse the nextLink %s naming %q: %v", l.link, l.host, err)
		}
		// the two well-known resolutions and the one listing page, and nothing else
		got := hitsFrom(n)
		if len(got) != 3 {
			t.Errorf("FolderTree made %d request(s) around %s, want 3: %v", len(got), l.link, got)
		}
		for _, h := range got {
			if !strings.HasPrefix(h, "graph.example.test/v1.0/me/mailFolders") {
				t.Errorf("a request left the base for %s: %s", l.link, h)
			}
		}
	}
	mu.Lock()
	treeLink = ""
	mu.Unlock()

	// a caller's guard: a link it has fetched, and a page past the cap (3)
	g := c.NewPageGuard()
	for i, want := range []ListingKind{"", ListingRepeat} {
		_, _, err := c.MessagePage(ctx, "ignored", "F_IN", base+"/me/mailFolders/F_IN/messages?$skiptoken=R", PageOpts{Guard: g})
		var le *ListingError
		switch {
		case want == "" && err != nil:
			t.Fatalf("the guard refused a first link: %v", err)
		case want != "" && (!errors.As(err, &le) || le.Kind != want):
			t.Errorf("fetch %d of one link through one guard is not a %s stop: %v", i+1, want, err)
		}
	}
	g = c.NewPageGuard()
	var capErr error
	for i := 0; i < 4; i++ {
		_, _, capErr = c.MessagePage(ctx, "ignored", "F_IN", base+"/me/mailFolders/F_IN/messages?$skiptoken=P"+string(rune('a'+i)), PageOpts{Guard: g})
		if i < 3 && capErr != nil {
			t.Fatalf("page %d of 3 was refused: %v", i+1, capErr)
		}
	}
	var le *ListingError
	if !errors.As(capErr, &le) || le.Kind != ListingPageCap || le.Limit != 3 {
		t.Errorf("the fourth page through one guard is not a page-cap stop at 3: %v", capErr)
	}

	// a 302 from the base to another server: zero requests there
	foreign := newForeignHost(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc("/me/mailFolders/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			http.Redirect(w, r, foreign.URL+r.URL.Path, http.StatusFound)
			return
		}
		http.NotFound(w, r) // the well-known resolutions
	})
	mux.HandleFunc("/me/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+r.URL.Path, http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	rc := meClient(t, srv, Config{}, time.Now().Add(time.Hour))
	var re *RedirectError
	if _, _, err := rc.MessagePage(ctx, "ignored", "F_IN", "", PageOpts{Top: 100}); !errors.As(err, &re) {
		t.Errorf("MessagePage followed a redirect off the base: %v", err)
	}
	if _, err := rc.FolderTree(ctx, "ignored", 0); !errors.As(err, &re) {
		t.Errorf("FolderTree followed a redirect off the base: %v", err)
	}
	if got := foreign.hits(); len(got) != 0 {
		t.Errorf("the foreign server received %d request(s), with Authorization %q", len(got), got)
	}
}
