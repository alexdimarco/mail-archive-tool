package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/localweb"
)

// The workspace's pages, routes, picker and settings, through the dashboard's own handler.

// get makes a GET as a browser on the loopback address does.
func (e *wsEnv) get(path string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1:8097"
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

// workspaceRoutes is every route the guarded-POST rule covers, from the dashboard's own
// route registry.
func workspaceRoutes(t *testing.T, h http.Handler) []string {
	t.Helper()
	var out []string
	for _, p := range localweb.RoutesOf(h) {
		if strings.HasPrefix(p, "/api/workspace/") || strings.HasPrefix(p, "/api/exports/") || strings.HasPrefix(p, "/api/m365/") {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return assure.Reached(t, out, "the workspace's routes")
}

// covers: MA-405, R19, R12, S41, S52
// Every /api/workspace/*, /api/exports/* and /api/m365/* route: GET is 405; a POST missing
// the token, with a wrong one, with a foreign Origin or with a non-loopback Host is 403
// with the side effect absent (no Graph request, no file, no dialog, no journal line);
// every JSON reply carries nosniff and no-store; and a body over 1 MiB is a 413 worded as
// an R-j refusal, never a bare status. The positive twin, a guarded POST, first.
func TestWorkspaceRoutesAreGuardedPosts(t *testing.T) {
	e := newWsEnv(t)
	dialogs := 0
	e.d.pick = func(ctx context.Context, title string) (string, error) { dialogs++; return e.dest, nil }
	routes := workspaceRoutes(t, e.h)
	// 19 Extraction-workspace routes + the always-on /api/m365/configure (core M365 setup);
	// every one is verified as a guarded POST below.
	if len(routes) != 20 {
		t.Fatalf("the workspace registers %d routes, want 20: %v", len(routes), routes)
	}
	body := `{"parent":"` + strings.ReplaceAll(e.dest, `\`, `\\`) + `","name":"made","runId":"x","folder":"F-INBOX","previewId":"x","action":"gone","path":"/"}`
	do := func(method, route string, mut func(*http.Request), payload string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, route, strings.NewReader(payload))
		req.Host = "127.0.0.1:8097"
		req.Header.Set("Origin", "http://127.0.0.1:8097")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", e.d.csrf)
		req.Header.Set("X-Epoch", e.d.boot)
		if mut != nil {
			mut(req)
		}
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		return rec
	}
	headers := func(route, what string, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Cache-Control") != "no-store" ||
			!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s %s: headers %v, want nosniff, no-store and JSON", route, what, rec.Header())
		}
	}

	// the positive twin: a guarded POST is answered, not refused by the guard
	if rec := do(http.MethodPost, "/api/workspace/source", nil, "{}"); rec.Code != http.StatusOK {
		t.Fatalf("the positive twin: %d %s", rec.Code, rec.Body.String())
	}

	reqs := len(e.fg.requests())
	before := treeState(t, e.dir)
	for _, route := range routes {
		rec := do(http.MethodGet, route, nil, "")
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
			t.Errorf("GET %s: %d (Allow %q), want 405 with Allow: POST", route, rec.Code, rec.Header().Get("Allow"))
		}
		headers(route, "GET", rec)
		for what, mut := range map[string]func(*http.Request){
			"no token":         func(r *http.Request) { r.Header.Del("X-CSRF-Token") },
			"a wrong token":    func(r *http.Request) { r.Header.Set("X-CSRF-Token", "not-the-token") },
			"a foreign Origin": func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") },
			"a foreign Host":   func(r *http.Request) { r.Host = "evil.example:8097" },
		} {
			rec := do(http.MethodPost, route, mut, body)
			var out map[string]any
			json.Unmarshal(rec.Body.Bytes(), &out)
			msg, _ := out["error"].(string)
			assure.Refused(t, rec.Code, msg, assure.Code(http.StatusForbidden), assure.Names("refused"))
			if out["id"] == "R-y" {
				t.Errorf("POST %s with %s was told it was a restart: %v", route, what, out)
			}
			headers(route, what, rec)
		}
	}
	if got := len(e.fg.requests()); got != reqs {
		t.Errorf("a refused POST reached Graph: %v", e.fg.requests()[reqs:])
	}
	if dialogs != 0 {
		t.Errorf("a refused POST opened %d folder dialog(s)", dialogs)
	}
	if got := treeState(t, e.dir); !reflect.DeepEqual(got, before) {
		t.Errorf("a refused POST wrote a file or a journal line: %v", diffKeys(before, got))
	}
	if _, err := os.Stat(e.state); !os.IsNotExist(err) {
		t.Errorf("a refused POST made the state directory: %v", err)
	}

	// a body over 1 MiB is a 413 worded as R-j on every route, with the headers
	huge := `{"dest":"` + strings.Repeat("x", 1<<20+10) + `"}`
	for _, route := range routes {
		rec := do(http.MethodPost, route, nil, huge)
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		refusedAs(t, rec.Code, http.StatusRequestEntityTooLarge, out, "R-j", "Select current folder")
		headers(route, "a 1 MiB body", rec)
	}
	if got := len(e.fg.requests()); got != reqs {
		t.Errorf("an oversized body reached Graph: %v", e.fg.requests()[reqs:])
	}
}

// bound lists the element ids a script looks up: $('id') and show('id', ...).
func bound(src string) []string {
	var out []string
	for _, pre := range []string{"$('", "show('"} {
		s := src
		for {
			i := strings.Index(s, pre)
			if i < 0 {
				break
			}
			s = s[i+len(pre):]
			if j := strings.Index(s, "'"); j >= 0 {
				out = append(out, s[:j])
				s = s[j:]
			}
		}
	}
	return out
}

// covers: MA-407, R19, S52
// Structural: workspace.js, exports.js and the sign-in script the pages share hold no
// innerHTML, outerHTML, insertAdjacentHTML, document.write, eval or Function(; the pages
// carry no inline on* handler and no inline script; every element id a script binds exists
// in its page; and a folder name or subject holding <img src=x onerror=...> from the fake
// Graph comes back as a JSON-escaped string, with nosniff and no-store (X15).
func TestWorkspaceScriptsAndPagesAreInert(t *testing.T) {
	e := newWsEnv(t)
	for name, src := range map[string]string{"workspace.js": workspaceJS, "exports.js": exportsJS, "signin.js": signinJS, "encryption.js": encryptionJS} {
		for _, bad := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "Function(", "setTimeout('", `setTimeout("`} {
			if strings.Contains(src, bad) {
				t.Errorf("%s contains %q", name, bad)
			}
		}
		if rec := e.get("/" + name); rec.Code != http.StatusOK || rec.Body.String() != src || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
			t.Errorf("GET /%s: %d %q", name, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	pages := map[string]map[string]bool{}
	for _, p := range []string{"/workspace", "/exports"} {
		rec := e.get(p)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", p, rec.Code)
		}
		nav, ids, inline := sidebarNav(t, rec.Body.String())
		assure.Reached(t, nav, p+": its sidebar")
		if len(inline) > 0 {
			t.Errorf("%s has inline event handlers: %v", p, inline)
		}
		root, _ := xhtml.Parse(strings.NewReader(rec.Body.String()))
		var walk func(n *xhtml.Node)
		walk = func(n *xhtml.Node) {
			if n.Type == xhtml.ElementNode && n.Data == "script" {
				src := ""
				for _, a := range n.Attr {
					if a.Key == "src" {
						src = a.Val
					}
				}
				if src == "" || n.FirstChild != nil || strings.HasPrefix(src, "/") || strings.Contains(src, "//") {
					t.Errorf("%s has a script that is inline or not relative: src=%q", p, src)
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(root)
		pages[p] = ids
	}
	for _, c := range []struct{ script, page string }{{workspaceJS, "/workspace"}, {signinJS, "/workspace"}, {exportsJS, "/exports"}} {
		for _, id := range bound(c.script) {
			if !pages[c.page][id] {
				t.Errorf("a script of %s binds #%s, which is not on the page", c.page, id)
			}
		}
	}

	// mail-derived text is a JSON string
	const evil = `<img src=x onerror=alert(1)>`
	e.fg.addFolder("F-EVIL", evil, "", 1)
	e.fg.mutate(func() { e.fg.folder("F-EVIL").Msgs[0].Subject = evil; e.fg.folder("F-EVIL").Msgs[0].FromName = evil })
	for _, call := range []struct {
		route string
		body  map[string]any
	}{
		{"/api/workspace/folders", nil},
		{"/api/workspace/messages", map[string]any{"folder": "F-EVIL"}},
	} {
		rec := e.postRaw(call.route, call.body)
		raw := rec.Body.String()
		if rec.Code != http.StatusOK || strings.Contains(raw, "<img") || !strings.Contains(raw, "\\u003cimg src=x onerror=alert(1)\\u003e") {
			t.Errorf("%s: %d %q: the markup is not a JSON-escaped string", call.route, rec.Code, raw)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s: headers %v", call.route, rec.Header())
		}
	}
	_, res := e.preview(e.selection(entry("F-EVIL", false)))
	if raw, _ := json.Marshal(res); strings.Contains(string(raw), "<img") {
		t.Errorf("the preview result carries raw markup: %s", raw)
	}
}

// covers: MA-408, R12, S41, S52
// Every dashboard page (Overview, workspace, Export activity) renders the sidebar and every
// entry resolves: a same-page anchor names an element that exists, a cross-page link hits a
// registered route whose page has the anchor; the Overview's Export group jumps to an Export
// card holding the relative links to the two pages (MA-302 holds the Overview's sidebar to
// same-page anchors); the Microsoft 365 card states own-mailbox-only and that shared
// mailboxes aren't available (the label stays "Microsoft 365 connection"); the workspace page
// has no mailbox-address input; and with the Export group the sidebar does not declare
// desktop P7 superseded (ruling ESC-5).
func TestEveryPageRendersTheSidebarAndEveryEntryResolves(t *testing.T) {
	e := newWsEnv(t)
	e.cfg.ReaderURL = "http://127.0.0.1:8099/"
	e.d = newDashboard(e.cfg)
	e.h = e.d.router()
	pageOf := map[string]string{"/": "", "/workspace": "", "/exports": ""}
	ids := map[string]map[string]bool{}
	navOf := map[string][]navLink{}
	for p := range pageOf {
		rec := e.get(p)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", p, rec.Code)
		}
		pageOf[p] = rec.Body.String()
		nav, pageIDs, inline := sidebarNav(t, pageOf[p])
		assure.Reached(t, nav, p+": its sidebar")
		navOf[p], ids[p] = nav, pageIDs
		if len(inline) > 0 {
			t.Errorf("%s: inline handlers %v", p, inline)
		}
	}
	for p, nav := range navOf {
		seen := map[string]bool{}
		for _, l := range nav {
			seen[l.text] = true
			switch {
			case l.text == "Open archive" || l.text == "Go back in time":
				if !strings.HasPrefix(l.href, "http://127.0.0.1:8099/") {
					t.Errorf("%s: %q is %q, want the reader", p, l.text, l.href)
				}
			case !l.isLink:
				t.Errorf("%s: %q is a disabled placeholder with a reader running", p, l.text)
			case strings.HasPrefix(l.href, "#"):
				if p != "/" || !ids[p][l.href[1:]] {
					t.Errorf("%s: %q links to %q, which this page does not hold", p, l.text, l.href)
				}
			case strings.HasPrefix(l.href, "./"):
				frag := strings.TrimPrefix(strings.TrimPrefix(l.href, "./"), "#")
				if frag != "" && !ids["/"][frag] {
					t.Errorf("%s: %q links to the Overview's %q, which it does not hold", p, l.text, l.href)
				}
			case l.href == "workspace" || l.href == "exports":
				if rec := e.get("/" + l.href); rec.Code != http.StatusOK {
					t.Errorf("%s: %q links to %s, which answers %d", p, l.text, l.href, rec.Code)
				}
			default:
				t.Errorf("%s: %q has the href %q, which is not an anchor, a relative page or the reader", p, l.text, l.href)
			}
			if strings.HasPrefix(l.href, "/") {
				t.Errorf("%s: %q links to the absolute path %q, which breaks under the per-launch prefix", p, l.text, l.href)
			}
		}
		for _, want := range []string{"Extraction workspace", "Export activity", "Microsoft 365 connection", "Archive now", "Status & health", "Scheduled backup"} {
			if !seen[want] {
				t.Errorf("%s: sidebar entry %q is missing", p, want)
			}
		}
	}
	// the Overview's Export group jumps to a card holding the two relative links
	card := pageOf["/"][strings.Index(pageOf["/"], `id="sec-export"`):]
	card = card[:strings.Index(card, "</div>")]
	if !strings.Contains(card, `href="workspace"`) || !strings.Contains(card, `href="exports"`) {
		t.Errorf("the Overview's Export card lacks the relative links to the two pages: %q", card)
	}
	// the Microsoft 365 card and the workspace say what they cover
	if p := pageOf["/"]; !strings.Contains(p, "Microsoft 365: your own mailbox only") || !strings.Contains(p, "Shared mailboxes") || !strings.Contains(p, "Microsoft 365 connection") {
		t.Error("the Overview's Microsoft 365 card does not state own-mailbox-only and that shared mailboxes aren't available")
	}
	if p := pageOf["/workspace"]; !strings.Contains(p, "Shared mailboxes aren't available") {
		t.Error("the workspace page does not say shared mailboxes aren't available")
	}
	root, _ := xhtml.Parse(strings.NewReader(pageOf["/workspace"]))
	var inputs func(n *xhtml.Node)
	inputs = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "input" {
			for _, a := range n.Attr {
				v := strings.ToLower(a.Val)
				if (a.Key == "type" && v == "email") || ((a.Key == "id" || a.Key == "name" || a.Key == "placeholder") && (strings.Contains(v, "mailbox") || strings.Contains(v, "email") || strings.Contains(v, "address"))) {
					t.Errorf("the workspace has a mailbox-address input: %v", n.Attr)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			inputs(c)
		}
	}
	inputs(root)
	for p, page := range pageOf {
		if strings.Contains(strings.ToLower(page), "superseded") || strings.Contains(page, "P7") {
			t.Errorf("%s declares desktop P7 superseded or names it", p)
		}
	}
}

// covers: MA-526, R12
// With the deployment switch off every workspace and export route, page and script is 404
// and the sidebar has no Export group and loads no sign-in script; with it on the routes
// and the group are there (the positive twin, first).
func TestDeploymentSwitchHidesTheWorkspace(t *testing.T) {
	on := newWsEnv(t)
	routes := workspaceRoutes(t, on.h)
	if rec := on.get("/"); !strings.Contains(rec.Body.String(), `>Export</div>`) || !strings.Contains(rec.Body.String(), `id="sec-export"`) {
		t.Fatal("with the switch on the Overview has no Export group")
	}
	if rec := on.get("/workspace"); rec.Code != http.StatusOK {
		t.Fatalf("with the switch on /workspace is %d", rec.Code)
	}
	if rec := on.postRaw("/api/workspace/source", "{}"); rec.Code != http.StatusOK {
		t.Fatalf("with the switch on a route answers %d", rec.Code)
	}

	off := newWsEnv(t, func(e *wsEnv) { e.cfg.WorkspaceOff = true })
	if rs := localweb.RoutesOf(off.h); len(rs) >= len(localweb.RoutesOf(on.h)) {
		t.Errorf("the switch left %d routes registered, want fewer than %d", len(rs), len(localweb.RoutesOf(on.h)))
	}
	for _, route := range routes {
		// Microsoft 365 configuration is core (Archive now needs it), not part of the
		// Extraction workspace, so it survives the switch — asserted separately below.
		if route == "/api/m365/configure" {
			continue
		}
		if rec := off.postRaw(route, "{}"); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s with the switch off: %d, want 404", route, rec.Code)
		}
		if rec := off.get(route); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s with the switch off: %d, want 404", route, rec.Code)
		}
	}
	// M365 configuration stays registered with the switch off (GET is 405, a guarded POST),
	// so a user can still set up Microsoft 365 for Archive now without the command line.
	if rec := off.get("/api/m365/configure"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("M365 configure must stay available with the switch off (GET 405), got %d", rec.Code)
	}
	for _, p := range []string{"/workspace", "/exports", "/workspace.js", "/exports.js", "/signin.js"} {
		if rec := off.get(p); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s with the switch off: %d, want 404", p, rec.Code)
		}
	}
	page := off.get("/").Body.String()
	nav, ids, _ := sidebarNav(t, page)
	assure.Reached(t, nav, "the sidebar with the switch off")
	for _, l := range nav {
		if strings.Contains(l.text, "Extraction") || strings.Contains(l.text, "Export") {
			t.Errorf("the sidebar still lists %q with the switch off", l.text)
		}
	}
	if ids["sec-export"] || strings.Contains(page, `>Export</div>`) || strings.Contains(page, "signin.js") || strings.Contains(page, `name="epoch"`) {
		t.Error("the Overview still carries the Export group, its card or its scripts with the switch off")
	}
	if off.d.cfg.StateDir != "" {
		if _, err := os.Stat(off.state); !os.IsNotExist(err) {
			t.Errorf("the switch off still made the state directory: %v", err)
		}
	}
}

// covers: MA-433, R29, R12, S52
// The folder picker: browse calls the picker seam with the directory option and returns the
// path; a cancel is {canceled}; an unavailable picker is {unavailable} with R-n's remedy; a
// second concurrent browse is refused ("a folder picker is already open"); an unguarded
// browse never invokes the seam; and the mocked zenity binary on Linux gets a clean argv.
func TestBrowseCallsThePickerSeam(t *testing.T) {
	e := newWsEnv(t)
	var mu sync.Mutex
	var titles []string
	var next func(ctx context.Context) (string, error)
	e.d.pick = func(ctx context.Context, title string) (string, error) {
		mu.Lock()
		titles = append(titles, title)
		fn := next
		mu.Unlock()
		return fn(ctx)
	}
	set := func(fn func(ctx context.Context) (string, error)) { mu.Lock(); next = fn; mu.Unlock() }

	set(func(context.Context) (string, error) { return e.dest, nil })
	out := e.ok("/api/workspace/browse", map[string]any{"title": "Pick it"})
	if out["path"] != e.dest || len(titles) != 1 || titles[0] != "Pick it" {
		t.Fatalf("a pick: %v with titles %v", out, titles)
	}
	e.ok("/api/workspace/browse", map[string]any{"title": strings.Repeat("t", 500)})
	if titles[1] != "Choose the folder to export into" {
		t.Errorf("an over-long title reached the dialog: %q", titles[1])
	}

	set(func(context.Context) (string, error) { return "", zenity.ErrCanceled })
	if out := e.ok("/api/workspace/browse", nil); out["canceled"] != true || out["path"] != nil {
		t.Errorf("a cancel: %v", out)
	}

	set(func(context.Context) (string, error) {
		return "", errors.New(`exec: "zenity": executable file not found in $PATH`)
	})
	code, out := e.post("/api/workspace/browse", nil)
	if out["unavailable"] != true || out["id"] != "R-n" || !strings.Contains(out["error"].(string), "zenity") || !strings.Contains(out["error"].(string), "Type the folder path") {
		t.Errorf("an unavailable picker: %d %v", code, out)
	}
	if strings.Contains(out["error"].(string), "executable file not found") {
		t.Errorf("the picker's raw error reached the page: %v", out["error"])
	}

	// a second concurrent browse is refused while the first is open
	opened, release := make(chan struct{}), make(chan struct{})
	set(func(context.Context) (string, error) { close(opened); <-release; return e.dest, nil })
	first := make(chan map[string]any, 1)
	go func() {
		rec := e.postRaw("/api/workspace/browse", nil)
		var o map[string]any
		json.Unmarshal(rec.Body.Bytes(), &o)
		first <- o
	}()
	<-opened
	calls := len(titles)
	_, out = e.post("/api/workspace/browse", nil)
	if out["busy"] != true || !strings.Contains(out["error"].(string), "A folder picker is already open") {
		t.Errorf("a second browse: %v", out)
	}
	mu.Lock()
	if len(titles) != calls {
		t.Error("a second browse opened a second dialog")
	}
	mu.Unlock()
	close(release)
	if o := <-first; o["path"] != e.dest {
		t.Errorf("the first browse: %v", o)
	}
	set(func(context.Context) (string, error) { return e.dest, nil })
	if out := e.ok("/api/workspace/browse", nil); out["path"] != e.dest {
		t.Errorf("a browse after the first closed: %v", out)
	}

	// an unguarded browse never invokes the seam
	n := len(titles)
	req := httptest.NewRequest(http.MethodPost, "/api/workspace/browse", strings.NewReader("{}"))
	req.Host = "127.0.0.1:8097"
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || len(titles) != n {
		t.Errorf("an unguarded browse: %d, dialogs %d -> %d", rec.Code, n, len(titles))
	}
}

// covers: MA-433, R29, S52
// The mocked zenity binary on Linux gets a clean argv: the directory option, the title as
// one argument however it is spelled, and nothing a shell would read.
func TestPickerArgvIsCleanOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("zenity is run as a program only on Linux; the other systems use their own dialogs")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "argv.log")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> '" + log + "'; done\necho '" + dir + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "zenity"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	const title = "a;b $(touch pwned) `id` \"q\""
	got, err := pickFolder(context.Background(), title)
	if err != nil || got != dir {
		t.Fatalf("the mocked zenity: %q, %v", got, err)
	}
	raw, _ := os.ReadFile(log)
	args := assure.Reached(t, strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n"), "zenity's argv")
	has := func(a string) bool {
		for _, x := range args {
			if x == a {
				return true
			}
		}
		return false
	}
	titled := has("--title=" + title)
	for i := 0; i+1 < len(args); i++ {
		titled = titled || (args[i] == "--title" && args[i+1] == title)
	}
	if !has("--file-selection") || !has("--directory") || !titled {
		t.Errorf("argv %q lacks the directory dialog or the title as one intact argument", args)
	}
	if _, err := os.Stat("pwned"); err == nil {
		os.Remove("pwned")
		t.Error("the title was run by a shell")
	}
}
