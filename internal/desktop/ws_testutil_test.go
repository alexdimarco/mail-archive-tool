package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/graphconfig"
)

// A fake Microsoft 365 for the Extraction workspace's tests: the device-code, token and
// /me endpoints, a mail folder tree with counts, paged and filtered message listings, and
// each message's /$value. Every request is recorded, so a test can say that only GETs were
// made and that nothing addressed /users/.

type fgMsg struct {
	ID       string
	Subject  string
	From     string
	FromName string
	Received time.Time
	Size     int
	Body     []byte
}

type fgFolder struct {
	ID     string
	Name   string
	Parent string
	Msgs   []fgMsg
	Bias   int // added to the reported totalItemCount, to make it differ from the listing
}

type fakeGraph struct {
	t   *testing.T
	srv *httptest.Server

	mu        sync.Mutex
	folders   []*fgFolder
	hits      []string
	methods   map[string]int
	pageSize  int // when set, no page holds more than this many messages
	ttl       int // the access token's expires_in seconds
	tokens    int // token endpoint requests
	valueFn   map[string]http.HandlerFunc
	upn       string
	counts    func(folder string, read int) int // overrides the reported count of a folder: read is the 1-based read number
	reads     map[string]int
	listFn    func(folder string, r *http.Request) // runs before a message listing is answered
	ignoreTop bool
	grants    []string // the grant_type of each token request
	tokenErr  string   // when set, the token endpoint refuses with this OAuth error code
}

// mutate runs fn with the fake's lock held: the way a test changes the mailbox while the
// dashboard's goroutines may be reading it.
func (f *fakeGraph) mutate(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func (f *fakeGraph) tokenCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokens
}

func (f *fakeGraph) readCount(folder string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads[folder]
}

func (f *fakeGraph) grantList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.grants...)
}

// removeFolder takes a folder out of the mailbox.
func (f *fakeGraph) removeFolder(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, fo := range f.folders {
		if fo.ID == id {
			f.folders = append(f.folders[:i], f.folders[i+1:]...)
			return
		}
	}
}

func newFakeGraph(t *testing.T) *fakeGraph {
	t.Helper()
	f := &fakeGraph{t: t, ttl: 3600, upn: "alice@contoso.org", valueFn: map[string]http.HandlerFunc{}, methods: map[string]int{}, reads: map[string]int{}}
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/devicecode", func(w http.ResponseWriter, r *http.Request) {
		j(w, map[string]any{"device_code": "DEV", "user_code": "WXYZ-1234", "verification_uri": "https://microsoft.com/devicelogin", "expires_in": 900, "interval": 1})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.tokens++
		f.grants = append(f.grants, r.PostForm.Get("grant_type"))
		ttl, n, fail := f.ttl, f.tokens, f.tokenErr
		f.mu.Unlock()
		if fail != "" { // the token endpoint refuses the refresh token
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":%q,"error_description":"AADSTS70008: the refresh token SECRET-RT-VALUE has expired"}`, fail)
			return
		}
		j(w, map[string]any{"access_token": fmt.Sprintf("AT%d", n), "refresh_token": "RT1", "token_type": "Bearer", "expires_in": ttl})
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		upn := f.upn
		f.mu.Unlock()
		j(w, map[string]any{"userPrincipalName": upn, "id": "OID1"})
	})
	mux.HandleFunc("/me/mailFolders", f.serveFolders)
	mux.HandleFunc("/me/mailFolders/", f.serveFolders)
	mux.HandleFunc("/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/me/messages/"), "/$value")
		f.mu.Lock()
		h := f.valueFn[id]
		f.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		w.Write(fgBody(id))
	})
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits = append(f.hits, r.Method+" "+r.URL.RequestURI())
		f.methods[r.Method]++
		f.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// fgBody is the message bytes the fake serves for id.
func fgBody(id string) []byte {
	return []byte("From: a@example.com\r\nSubject: subj-" + id + "\r\nMessage-ID: <" + strings.ToLower(id) + "@x>\r\n\r\nbody of " + id + "\r\n")
}

// addFolder adds a folder (parent "" = a top-level folder) holding n messages, newest first.
func (f *fakeGraph) addFolder(id, name, parent string, n int) *fgFolder {
	fo := &fgFolder{ID: id, Name: name, Parent: parent}
	base := time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		mid := fmt.Sprintf("%s-%04d", id, i+1)
		fo.Msgs = append(fo.Msgs, fgMsg{ID: mid, Subject: "subject " + mid, From: "a@example.com", FromName: "A", Received: base.Add(-time.Duration(i) * time.Hour), Size: len(fgBody(mid))})
	}
	f.mu.Lock()
	f.folders = append(f.folders, fo)
	f.mu.Unlock()
	return fo
}

func (f *fakeGraph) folder(id string) *fgFolder {
	for _, fo := range f.folders {
		if fo.ID == id {
			return fo
		}
	}
	return nil
}

func (f *fakeGraph) kids(id string) []*fgFolder {
	var out []*fgFolder
	for _, fo := range f.folders {
		if fo.Parent == id {
			out = append(out, fo)
		}
	}
	return out
}

func (f *fakeGraph) reported(fo *fgFolder) int {
	f.reads[fo.ID]++
	n := len(fo.Msgs) + fo.Bias
	if f.counts != nil {
		n = f.counts(fo.ID, f.reads[fo.ID])
	}
	return n
}

func (f *fakeGraph) serveFolders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	f.mu.Lock()
	defer f.mu.Unlock()
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/me/mailFolders"), "/")
	parts := strings.Split(rest, "/")
	enc := json.NewEncoder(w)
	folderJSON := func(fo *fgFolder) map[string]any {
		parent := fo.Parent
		if parent == "" {
			parent = "ROOT"
		}
		return map[string]any{"id": fo.ID, "displayName": fo.Name, "parentFolderId": parent, "childFolderCount": len(f.kids(fo.ID)), "totalItemCount": len(fo.Msgs) + fo.Bias}
	}
	switch {
	case rest == "":
		var vs []map[string]any
		for _, fo := range f.kids("") {
			vs = append(vs, folderJSON(fo))
		}
		enc.Encode(map[string]any{"value": vs})
	case len(parts) == 1 && (parts[0] == "deleteditems" || parts[0] == "junkemail"):
		id := map[string]string{"deleteditems": "F-DEL", "junkemail": "F-JUNK"}[parts[0]]
		if f.folder(id) == nil {
			http.Error(w, `{"error":{"code":"ErrorItemNotFound"}}`, http.StatusNotFound)
			return
		}
		enc.Encode(map[string]any{"id": id})
	case len(parts) == 1:
		fo := f.folder(parts[0])
		if fo == nil {
			http.Error(w, `{"error":{"code":"ErrorItemNotFound"}}`, http.StatusNotFound)
			return
		}
		enc.Encode(map[string]any{"id": fo.ID, "totalItemCount": f.reported(fo)})
	case len(parts) == 2 && parts[1] == "childFolders":
		var vs []map[string]any
		for _, fo := range f.kids(parts[0]) {
			vs = append(vs, folderJSON(fo))
		}
		enc.Encode(map[string]any{"value": vs})
	case len(parts) == 2 && parts[1] == "messages":
		fo := f.folder(parts[0])
		if fo == nil {
			http.Error(w, `{"error":{"code":"ErrorItemNotFound"}}`, http.StatusNotFound)
			return
		}
		fn := f.listFn
		f.mu.Unlock()
		if fn != nil {
			fn(fo.ID, r)
		}
		f.mu.Lock()
		f.serveMessages(w, r, fo)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeGraph) serveMessages(w http.ResponseWriter, r *http.Request, fo *fgFolder) {
	q := r.URL.Query()
	msgs := append([]fgMsg(nil), fo.Msgs...)
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].Received.After(msgs[j].Received) })
	if flt := q.Get("$filter"); flt != "" {
		if i := strings.Index(flt, " le "); i > 0 {
			if t, err := time.Parse(time.RFC3339, strings.TrimSpace(flt[i+4:])); err == nil {
				var keep []fgMsg
				for _, m := range msgs {
					if !m.Received.After(t) {
						keep = append(keep, m)
					}
				}
				msgs = keep
			}
		}
	}
	top := 1000
	if v, err := strconv.Atoi(q.Get("$top")); err == nil && v > 0 {
		top = v
	}
	if f.pageSize > 0 && top > f.pageSize {
		top = f.pageSize
	}
	if f.ignoreTop { // a misbehaving server that answers the whole folder at once
		top = len(msgs) + 1
	}
	skip, _ := strconv.Atoi(q.Get("skip"))
	if skip > len(msgs) {
		skip = len(msgs)
	}
	page := msgs[skip:min(skip+top, len(msgs))]
	var vs []map[string]any
	for _, m := range page {
		v := map[string]any{"id": m.ID, "internetMessageId": "<" + strings.ToLower(m.ID) + "@x>", "subject": m.Subject,
			"from":             map[string]any{"emailAddress": map[string]string{"address": m.From, "name": m.FromName}},
			"receivedDateTime": m.Received.UTC().Format(time.RFC3339), "parentFolderId": fo.ID, "hasAttachments": false}
		if m.Size >= 0 { // a negative size is a message whose size the tenant does not report
			v["singleValueExtendedProperties"] = []map[string]string{{"id": "Integer 0x0E08", "value": strconv.Itoa(m.Size)}}
		}
		vs = append(vs, v)
	}
	out := map[string]any{"value": vs}
	if skip+top < len(msgs) {
		nq := url.Values{}
		for k, v := range q {
			nq[k] = v
		}
		nq.Set("skip", strconv.Itoa(skip+top))
		out["@odata.nextLink"] = "http://" + r.Host + r.URL.Path + "?" + nq.Encode()
	}
	w.Header().Set("Preference-Applied", `IdType="ImmutableId"`)
	json.NewEncoder(w).Encode(out)
}

// requests is a copy of every request the fake has seen.
func (f *fakeGraph) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.hits...)
}

func (f *fakeGraph) count(prefix string) int {
	n := 0
	for _, h := range f.requests() {
		if strings.Contains(h, prefix) {
			n++
		}
	}
	return n
}

// wsEnv is a dashboard wired to a fake Microsoft 365, a saved sign-in, a state
// directory and a destination, with helpers to make the guarded POSTs the page makes.
type wsEnv struct {
	t     *testing.T
	fg    *fakeGraph
	d     *dashboard
	h     http.Handler
	cfg   Config
	dir   string
	state string
	dest  string
	out   string // the configured archive location
	tok   string
	life  *Lifecycle
	stop  context.CancelFunc
}

type wsOpt func(*wsEnv)

// newWsEnv builds the environment: a configured, signed-in alice, a fake tree
// (Inbox with 3 subfolders, Sent Items, Deleted Items), a state directory and a
// destination.
func newWsEnv(t *testing.T, opts ...wsOpt) *wsEnv {
	t.Helper()
	e := &wsEnv{t: t, fg: newFakeGraph(t), dir: t.TempDir()}
	e.state, e.dest, e.out = filepath.Join(e.dir, "state"), filepath.Join(e.dir, "dest"), filepath.Join(e.dir, "archive")
	for _, d := range []string{e.dest, e.out} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.fg.addFolder("F-INBOX", "Inbox", "", 7)
	e.fg.addFolder("F-CLIENTS", "Clients", "F-INBOX", 4)
	e.fg.addFolder("F-ACME", "Acme", "F-CLIENTS", 2)
	e.fg.addFolder("F-SENT", "Sent Items", "", 5)
	e.fg.addFolder("F-DEL", "Deleted Items", "", 3)
	cfgPath := filepath.Join(e.dir, "graph-config.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "app"}); err != nil { // app: the workspace still signs in as the user
		t.Fatal(err)
	}
	e.tok = filepath.Join(e.dir, "tok.json")
	e.writeToken("alice@contoso.org", time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	e.stop = cancel
	e.life = NewLifecycle(ctx)
	e.cfg = Config{ConfigPath: cfgPath, Out: e.out, SettingsPath: filepath.Join(e.dir, "settings.json"), TokenCachePath: e.tok,
		BaseURL: e.fg.srv.URL, TokenURL: e.fg.srv.URL + "/token", DeviceAuthURL: e.fg.srv.URL + "/devicecode",
		StateDir: e.state, Host: "testhost", Version: "vTEST", Lifecycle: e.life, Account: `DESKTOP-7\alex`,
		BackupInstalled: func() bool { return false },
		RequestTimeout:  5 * time.Second, MIMEIdleTimeout: 2 * time.Second, MIMEHeaderTimeout: 2 * time.Second}
	for _, o := range opts {
		o(e)
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-e.life.Drained():
		case <-time.After(20 * time.Second):
			t.Error("the lifecycle did not drain")
		}
	})
	e.d = newDashboard(e.cfg)
	e.h = e.d.router()
	// A test never opens a real dialog or starts a real file manager: both seams fail loudly
	// unless the test replaces them.
	e.d.pick = func(ctx context.Context, title string) (string, error) {
		e.t.Errorf("a test opened the real folder dialog (%q)", title)
		return "", errors.New("no dialog in tests")
	}
	e.d.reveal = func(name string, args []string) error {
		e.t.Errorf("a test started a real program: %s %v", name, args)
		return errors.New("no programs in tests")
	}
	return e
}

// writeToken saves alice's (or another account's) sign-in, valid for ttl.
func (e *wsEnv) writeToken(upn string, ttl time.Duration) {
	body := `{"upn":"` + upn + `","token":{"access_token":"AT0","refresh_token":"RT0","token_type":"Bearer","expiry":"` + time.Now().Add(ttl).UTC().Format(time.RFC3339Nano) + `"}}`
	if err := os.WriteFile(e.tok, []byte(body), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

// postRaw makes a guarded POST as the page does (the CSRF token, a loopback Origin and
// Host, the epoch) and returns the recorder.
func (e *wsEnv) postRaw(path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.postCtx(context.Background(), path, body)
}

// postCtx is postRaw with the request carrying ctx.
func (e *wsEnv) postCtx(ctx context.Context, path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	var b []byte
	switch v := body.(type) {
	case nil:
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		b, _ = json.Marshal(v)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b)).WithContext(ctx)
	req.Host = "127.0.0.1:8097"
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", e.d.csrf)
	req.Header.Set("X-Epoch", e.d.boot)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

// post makes the POST and decodes its JSON reply.
func (e *wsEnv) post(path string, body any) (int, map[string]any) {
	e.t.Helper()
	rec := e.postRaw(path, body)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		e.t.Fatalf("%s answered %d with a body that is not JSON: %q", path, rec.Code, rec.Body.String())
	}
	return rec.Code, out
}

// ok posts and requires a 200 with ok:true.
func (e *wsEnv) ok(path string, body any) map[string]any {
	e.t.Helper()
	code, out := e.post(path, body)
	if code != http.StatusOK || out["ok"] != true {
		e.t.Fatalf("%s: %d %v", path, code, out)
	}
	return out
}

// folders loads the folder tree and returns its ids by name.
func (e *wsEnv) folders() map[string]string {
	e.t.Helper()
	out := e.ok("/api/workspace/folders", nil)
	ids := map[string]string{}
	for _, f := range out["folders"].([]any) {
		m := f.(map[string]any)
		ids[m["name"].(string)] = m["id"].(string)
	}
	return ids
}

// rows lists a folder's first page and returns the row handles.
func (e *wsEnv) rows(folder string) (handles []string, out map[string]any) {
	e.t.Helper()
	out = e.ok("/api/workspace/messages", map[string]any{"folder": folder})
	for _, r := range out["rows"].([]any) {
		handles = append(handles, r.(map[string]any)["h"].(string))
	}
	return handles, out
}

// selection is a selection description of whole folders.
func (e *wsEnv) selection(entries ...map[string]any) map[string]any {
	sel := map[string]any{"source": map[string]any{"kind": "m365", "mailbox": "alice@contoso.org"}, "dest": e.dest,
		"options": map[string]any{"preserve": true}, "folders": entries, "messages": []any{}}
	return sel
}

func entry(id string, subtree bool, exclude ...string) map[string]any {
	if exclude == nil {
		exclude = []string{}
	}
	return map[string]any{"id": id, "subtree": subtree, "exclude": exclude}
}

// preview starts a preview and waits for its result.
func (e *wsEnv) preview(sel map[string]any) (id string, result map[string]any) {
	e.t.Helper()
	out := e.ok("/api/workspace/preview", sel)
	id = out["previewId"].(string)
	return id, e.waitPreview(id)
}

func (e *wsEnv) waitPreview(id string) map[string]any {
	e.t.Helper()
	for i := 0; i < 300; i++ {
		_, out := e.post("/api/workspace/preview-status", map[string]any{"previewId": id})
		switch out["state"] {
		case "done":
			return out["result"].(map[string]any)
		case "failed":
			e.t.Fatalf("the preview failed: %v", out)
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatal("the preview never finished")
	return nil
}

// waitRun waits until no export is running.
func (e *wsEnv) waitRun() map[string]any {
	e.t.Helper()
	for i := 0; i < 600; i++ {
		_, out := e.post("/api/exports/progress", nil)
		if out["running"] == false {
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatal("the export never finished")
	return nil
}

// quickDone waits for the lifecycle's drain after cancelling its root.
func (e *wsEnv) shutdown() {
	e.stop()
	<-e.life.Drained()
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var _ = io.Discard
