package graphtest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Listing is how one of the fake's paging endpoints pages.
type Listing int

const (
	Pages   Listing = iota // its pages, the last one with no nextLink
	Repeat                 // from page 2 on, each page names itself as the next one
	Endless                // after page 1, empty pages with fresh nextLinks for ever
)

// Server is a fake Microsoft token endpoint and Graph API for app-only capture
// (/users/{upn}), shared by the tests that drive the shipped capture path
// (app.RunGraph) and the cmd/mailarchive exit-code test (MA-540). Each mailbox
// holds flat folders whose message listings page pageSize messages at a time,
// and each message serves Body as its $value. A test makes a listing or a $value
// misbehave (SetListing, SetValue) and asks what was requested (Requests). The
// well-known Deleted Items and Junk Email resolutions answer 404, so a walk
// excludes nothing.
type Server struct {
	URL string

	srv    *httptest.Server
	mu     sync.Mutex
	boxes  map[string]*mailbox
	values map[string]http.HandlerFunc
	reqs   []string
}

type mailbox struct {
	folders []*folder
	listing Listing // the folder list's
}

type folder struct {
	id, name string
	pageSize int
	msgs     []string
	listing  Listing
}

// NewServer starts an empty fake.
func NewServer() *Server {
	s := &Server{boxes: map[string]*mailbox{}, values: map[string]http.HandlerFunc{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	s.URL = s.srv.URL
	return s
}

// Close shuts the fake down.
func (s *Server) Close() { s.srv.Close() }

func (s *Server) box(upn string) *mailbox {
	b := s.boxes[upn]
	if b == nil {
		b = &mailbox{}
		s.boxes[upn] = b
	}
	return b
}

// AddFolder adds a top-level folder to upn's mailbox, listing pageSize messages
// to a page.
func (s *Server) AddFolder(upn, id, name string, pageSize int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.box(upn)
	b.folders = append(b.folders, &folder{id: id, name: name, pageSize: pageSize})
}

// AddMessage lists message id last in upn's folder. Message ids are unique
// across the fake's mailboxes.
func (s *Server) AddMessage(upn, folderID, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.folder(upn, folderID); f != nil {
		f.msgs = append(f.msgs, id)
	}
}

func (s *Server) folder(upn, id string) *folder {
	for _, f := range s.box(upn).folders {
		if f.id == id {
			return f
		}
	}
	return nil
}

// SetListing sets how upn's folder listing pages; folderID "" is the mailbox's
// folder list.
func (s *Server) SetListing(upn, folderID string, l Listing) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if folderID == "" {
		s.box(upn).listing = l
		return
	}
	if f := s.folder(upn, folderID); f != nil {
		f.listing = l
	}
}

// SetValue serves message id's $value through h (one of the misbehaving
// handlers) instead of its Body.
func (s *Server) SetValue(id string, h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[id] = h
}

// ResetRequests forgets the requests recorded so far.
func (s *Server) ResetRequests() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = nil
}

// Requests counts the recorded requests whose path starts with prefix.
func (s *Server) Requests(prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, p := range s.reqs {
		if strings.HasPrefix(p, prefix) {
			n++
		}
	}
	return n
}

func writeJSON(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, s)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/token" {
		writeJSON(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
		return
	}
	s.mu.Lock()
	s.reqs = append(s.reqs, r.URL.Path)
	s.mu.Unlock()
	// /users/{upn}/mailFolders[/{id}[/messages]] or /users/{upn}/messages/{id}/$value
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/users/"), "/")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	switch {
	case len(parts) == 2 && parts[1] == "mailFolders":
		s.folderList(w, r, parts[0], page)
	case len(parts) == 4 && parts[1] == "mailFolders" && parts[3] == "messages":
		s.messages(w, r, parts[0], parts[2], page)
	case len(parts) == 4 && parts[1] == "messages" && parts[3] == "$value":
		s.mu.Lock()
		h := s.values[parts[2]]
		s.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		io.WriteString(w, Body(parts[2]))
	default:
		http.NotFound(w, r) // the well-known folder resolutions, and anything else
	}
}

// next is the nextLink a page of mode l carries: "" for none.
func next(r *http.Request, l Listing, page int, more bool) string {
	n := page + 1
	switch {
	case l == Repeat && page >= 2:
		n = page
	case l == Pages && !more:
		return ""
	}
	return "http://" + r.Host + r.URL.Path + "?page=" + strconv.Itoa(n)
}

func writePage(w http.ResponseWriter, values []string, nextLink string) {
	body := `{"value":[` + strings.Join(values, ",") + `]`
	if nextLink != "" {
		body += `,"@odata.nextLink":"` + nextLink + `"`
	}
	writeJSON(w, body+"}")
}

func (s *Server) folderList(w http.ResponseWriter, r *http.Request, upn string, n int) {
	s.mu.Lock()
	b := s.box(upn)
	var vals []string
	if n == 1 {
		for _, f := range b.folders {
			vals = append(vals, `{"id":"`+f.id+`","displayName":"`+f.name+`","childFolderCount":0}`)
		}
	}
	l := b.listing
	s.mu.Unlock()
	writePage(w, vals, next(r, l, n, false))
}

func (s *Server) messages(w http.ResponseWriter, r *http.Request, upn, folderID string, n int) {
	s.mu.Lock()
	f := s.folder(upn, folderID)
	if f == nil {
		s.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	l := f.listing
	var ids []string
	if l != Endless || n == 1 {
		if lo := (n - 1) * f.pageSize; lo < len(f.msgs) {
			ids = append(ids, f.msgs[lo:min(lo+f.pageSize, len(f.msgs))]...)
		}
	}
	more := n*f.pageSize < len(f.msgs)
	s.mu.Unlock()
	received := time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	var vals []string
	for _, id := range ids {
		vals = append(vals, `{"id":"`+id+`","internetMessageId":"<`+strings.ToLower(id)+`@x>","subject":"subj-`+id+
			`","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"`+received+`"}`)
	}
	writePage(w, vals, next(r, l, n, more))
}

// MA-540's scaled bounds (fault-isolation §9): the JSON request timeout, the
// $value idle and header windows, capture's overall cap (Config.MIMETimeout
// until S3) and the page cap, which the tests pass as the client's overrides.
const (
	RequestTimeout = 100 * time.Millisecond
	IdleTimeout    = 200 * time.Millisecond
	HeaderTimeout  = 600 * time.Millisecond
	OverallCap     = 2 * time.Second
	PageCap        = 5
)

// Stop is one of MA-540's stop cases: Apply makes mailbox u1 of the Fixture
// misbehave before the case run, which must then fail that mailbox with a
// typed error naming the stop.
type Stop struct {
	Name  string
	Apply func(s *Server)
}

// NewItem is the message the $value stop cases add to u1's Inbox; its download
// is what stops.
const NewItem = "U1NEW"

// Fixture loads MA-540's two mailboxes, u1 and u2: each has an Inbox of three
// messages one to a page, an Archive of one and a Sent of two, one to a page.
// A walk is then 7 listing pages per mailbox and no loop more than 3.
func (s *Server) Fixture() {
	for _, u := range []string{"u1", "u2"} {
		up := strings.ToUpper(u)
		s.AddFolder(u, "F_IN", "Inbox", 1)
		s.AddFolder(u, "F_AR", "Archive", 1)
		s.AddFolder(u, "F_SE", "Sent", 1)
		for i := 1; i <= 3; i++ {
			s.AddMessage(u, "F_IN", up+"IN"+strconv.Itoa(i))
		}
		s.AddMessage(u, "F_AR", up+"AR1")
		s.AddMessage(u, "F_SE", up+"SE1")
		s.AddMessage(u, "F_SE", up+"SE2")
	}
}

// valueStop adds NewItem to u1's Inbox, its $value served by h.
func valueStop(h http.HandlerFunc) func(s *Server) {
	return func(s *Server) {
		s.AddMessage("u1", "F_IN", NewItem)
		s.SetValue(NewItem, h)
	}
}

// Stops are MA-540's stop cases on the Fixture, with its scaled bounds (above):
// (1) a
// repeating nextLink in u1's Inbox after its first page; (2) fresh nextLinks
// past the page cap in u1's Inbox, and in u1's folder list; (3) NewItem's
// $value stalling mid-body past the idle window, sending no headers within the
// header window, or trickling past the overall cap. Case (4), a $value over the
// size cap, is not here: capture passes no size cap (owner ruling RR6-OR1).
var Stops = []Stop{
	{"repeating nextLink in Messages", func(s *Server) { s.SetListing("u1", "F_IN", Repeat) }},
	{"page cap in Messages", func(s *Server) { s.SetListing("u1", "F_IN", Endless) }},
	{"page cap in Folders", func(s *Server) { s.SetListing("u1", "", Endless) }},
	{"$value idle", valueStop(StallMidBody("From: a@example.com\r\n\r\npartial"))},
	{"$value header", valueStop(NoHeaders())},
	{"$value overall", valueStop(Trickle(50*time.Millisecond, -1))},
}
