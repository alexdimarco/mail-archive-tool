package graph

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// recorder keeps every request a fake workspace server receives, so a test can
// prove what the client addressed and that it only ever read.
type recorder struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (r *recorder) add(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, req.Clone(context.Background()))
}

func (r *recorder) all() []*http.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*http.Request(nil), r.reqs...)
}

// onlyMeGETs fails the test unless every recorded request was a GET to a /me
// path under prefix: nothing wrote, and nothing addressed /users/.
func (r *recorder) onlyMeGETs(t *testing.T, prefix string) {
	t.Helper()
	got := assure.Reached(t, r.all(), "recorded requests")
	for _, q := range got {
		if q.Method != http.MethodGet {
			t.Errorf("a non-GET request: %s %s", q.Method, q.URL.RequestURI())
		}
		if strings.HasPrefix(q.URL.Path, "/users/") || !strings.HasPrefix(q.URL.Path, prefix) {
			t.Errorf("a request outside %s: %s", prefix, q.URL.RequestURI())
		}
	}
}

// folderFake serves a /me mailbox's folder tree:
//
//	Inbox (F_IN, 5 items, one child: Sub F_SUB, 3 items), Junk Email (F_JUNK, 2),
//
// the top level split over two pages, the second holding Deleted Items (F_DEL, 0).
// The well-known names resolve to their ids, and /me/mailFolders/F_IN answers
// the count.
func folderFake(t *testing.T, rec *recorder) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	// total is the field a request's $select may leave out, as Graph does.
	total := func(r *http.Request, n string) string {
		if strings.Contains(r.URL.Query().Get("$select"), "totalItemCount") {
			return `,"totalItemCount":` + n
		}
		return ""
	}
	mux.HandleFunc("/me/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, `{"value":[{"id":"F_DEL","displayName":"Deleted Items","parentFolderId":"ROOT","childFolderCount":0`+total(r, "0")+`}]}`)
			return
		}
		vals := `{"id":"F_IN","displayName":"  <b>Inbox</b> ","parentFolderId":"ROOT","childFolderCount":1` + total(r, "5") + `},` +
			`{"id":"F_JUNK","displayName":"Junk Email","parentFolderId":"ROOT","childFolderCount":0` + total(r, "2") + `}`
		writeJSON(w, `{"value":[`+vals+`],"@odata.nextLink":"`+baseOf(r)+`/me/mailFolders?page=2&`+r.URL.RawQuery+`"}`)
	})
	mux.HandleFunc("/me/mailFolders/", func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		switch strings.TrimPrefix(r.URL.Path, "/me/mailFolders/") {
		case "deletedItems":
			writeJSON(w, `{"id":"F_DEL"}`)
		case "junkemail":
			writeJSON(w, `{"id":"F_JUNK"}`)
		case "F_IN":
			writeJSON(w, `{"id":"F_IN"`+total(r, "7")+`}`)
		case "F_IN/childFolders":
			writeJSON(w, `{"value":[{"id":"F_SUB","displayName":"Sub","parentFolderId":"F_IN","childFolderCount":0`+total(r, "3")+`}]}`)
		default:
			http.Error(w, `{"error":{"code":"ErrorItemNotFound","message":"gone"}}`, http.StatusNotFound)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// covers: MA-400, R17, R29, S52
// FolderTree on a delegated client addresses only /me/mailFolders and its
// childFolders, in one walk of every folder: Deleted Items and Junk Email are
// included and labelled through the well-known ids, the display name is the raw
// one (never trimmed or sanitized), parentFolderId, childFolderCount and
// totalItemCount ride along, the first request pages $top=100 with exactly the
// workspace's $select, and every request is a GET. A tree above the folder cap is
// refused with a typed error naming the cap and returns nothing; one exactly at
// the cap is the positive twin. FolderCount reads one folder's totalItemCount, a
// missing folder is a typed 404.
func TestFolderTreeContract(t *testing.T) {
	rec := &recorder{}
	srv := folderFake(t, rec)
	c := meClient(t, srv, Config{}, time.Now().Add(time.Hour))
	ctx := context.Background()

	got, err := c.FolderTree(ctx, "ignored", 10000)
	if err != nil {
		t.Fatal(err)
	}
	want := []FolderInfo{
		{ID: "F_IN", ParentID: "ROOT", DisplayName: "  <b>Inbox</b> ", ChildFolderCount: 1, TotalItemCount: 5},
		{ID: "F_SUB", ParentID: "F_IN", DisplayName: "Sub", TotalItemCount: 3},
		{ID: "F_JUNK", ParentID: "ROOT", DisplayName: "Junk Email", TotalItemCount: 2, WellKnown: WellKnownJunkEmail},
		{ID: "F_DEL", ParentID: "ROOT", DisplayName: "Deleted Items", WellKnown: WellKnownDeletedItems},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FolderTree =\n %+v\nwant\n %+v", got, want)
	}
	rec.onlyMeGETs(t, "/me/mailFolders")
	first := rec.all()
	var lists int
	for _, q := range first {
		if q.URL.Path != "/me/mailFolders" && !strings.HasSuffix(q.URL.Path, "/childFolders") {
			continue // the well-known resolutions
		}
		lists++
		if q.URL.Query().Get("page") != "" {
			continue // a followed nextLink, which the server built
		}
		if q.URL.Query().Get("$top") != "100" || q.URL.Query().Get("$select") != "id,displayName,parentFolderId,childFolderCount,totalItemCount" {
			t.Errorf("a first listing request carries $top/$select %q: %s", q.URL.RawQuery, q.URL.RequestURI())
		}
	}
	if lists != 3 {
		t.Errorf("the walk made %d listing requests, want 3 (two top-level pages and one childFolders)", lists)
	}

	// the folder cap: at the cap is fine, one above is a typed refusal
	if at, err := c.FolderTree(ctx, "ignored", 4); err != nil || len(at) != 4 {
		t.Fatalf("a tree exactly at the cap: %d folders, %v", len(at), err)
	}
	over, err := c.FolderTree(ctx, "ignored", 3)
	var fce *FolderCapError
	if !errors.As(err, &fce) || fce.Limit != 3 {
		t.Fatalf("a tree above the cap is not a *FolderCapError naming it: %v", err)
	}
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names("3", "folders"),
		assure.NoSideEffect(func() bool { return over == nil }))

	// FolderCount
	n, err := c.FolderCount(ctx, "ignored", "F_IN")
	if err != nil || n != 7 {
		t.Fatalf("FolderCount = %d, %v, want 7", n, err)
	}
	last := rec.all()[len(rec.all())-1]
	if last.URL.Path != "/me/mailFolders/F_IN" || last.URL.Query().Get("$select") != "totalItemCount" {
		t.Errorf("FolderCount asked %s", last.URL.RequestURI())
	}
	_, err = c.FolderCount(ctx, "ignored", "F_GONE")
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusNotFound {
		t.Errorf("a missing folder is not a typed 404: %v", err)
	}
	rec.onlyMeGETs(t, "/me/mailFolders")
}

// covers: MA-401, R17, R29, S52, S20
func TestMessagePageContract(t *testing.T) {
	t.Skip("MA-401: MessagePage is not built; lands in EW-E1")
}

// covers: MA-401, R17, R29, S52, S20
func TestWorkspaceNextLinkPin(t *testing.T) {
	t.Skip("MA-401: the host/path pin on the workspace loops is not switched on; lands in EW-E1")
}

// covers: MA-403, R17, R9, R29, S52
func TestWorkspaceClientIsDeviceMeOnly(t *testing.T) {
	t.Skip("MA-403: the workspace read paths are not built; lands in EW-E1")
}
