package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/localweb"
	"mail-archive-tool/internal/localweb/localwebtest"
)

// servedText GETs every route h names that serves the tool's own pages (not the
// archive's /files/) and returns the HTML and script bodies it got, by path.
func servedText(t *testing.T, h http.Handler) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, pattern := range localweb.RoutesOf(h) {
		if strings.HasPrefix(pattern, "/files/") {
			continue
		}
		req, err := localwebtest.Request(http.MethodGet, pattern)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = "127.0.0.1:8099"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		ct := rec.Header().Get("Content-Type")
		if rec.Code == http.StatusOK && (strings.HasPrefix(ct, "text/html") || strings.Contains(ct, "javascript")) {
			out[req.URL.Path] = rec.Body.String()
		}
	}
	return out
}

// covers: MA-372, R23, X9
// No HTML or script a surface serves names a same-origin absolute URL (src, href,
// action, fetch, template.URL): each would resolve against the server's root and
// not under /s/<P>/, where only the locked page answers [CC17]. The reader's page,
// its script and the go-back page, and the setup wizard's page and script — read
// from the handlers the binaries wrap, over a populated archive so go-back lists
// real links.
func TestServedPagesHaveNoSameOriginAbsoluteURL(t *testing.T) {
	a := newGBArchive(t)
	a.add("store\x00mid:alpha", "Alpha", "store/Inbox/alpha.html", "Inbox", "Inbox", true, gbD1, gbD2)
	a.save()
	reader := servedText(t, New(a.dir, a.ix))
	setup, _, _ := newSetup(t)
	wizard := servedText(t, setup)
	for _, c := range []struct {
		surface string
		pages   map[string]string
		want    []string
	}{
		{"reader", reader, []string{"/", "/app.js", "/goback"}},
		{"setup", wizard, []string{"/", "/setup.js"}},
	} {
		for _, p := range c.want {
			body, ok := c.pages[p]
			assure.Reached(t, body, c.surface+" page "+p)
			if !ok {
				t.Fatalf("%s: %s was not served", c.surface, p)
			}
			if bad := localwebtest.SameOriginAbsoluteURLs(body); len(bad) != 0 {
				t.Errorf("%s %s names same-origin absolute URLs %q; they must be relative to the page", c.surface, p, bad)
			}
		}
	}
}

// browse serves h behind a session on a loopback listener and loads entry as a
// browser would.
func browse(t *testing.T, surface string, h http.Handler, s *localweb.Session, entry string) []localwebtest.Loaded {
	t.Helper()
	ts := httptest.NewServer(localweb.Wrap(localweb.Surface{Name: surface, Session: s}, h))
	t.Cleanup(ts.Close)
	return localwebtest.Browse(t, ts.URL, s, entry)
}

func loadedHas(loaded []localwebtest.Loaded, suffix string) bool {
	for _, l := range loaded {
		if strings.HasSuffix(l.URL, suffix) && l.Status == http.StatusOK {
			return true
		}
	}
	return false
}

// covers: MA-372, R23, X9
// The reader, as `serve` and the desktop serve it, loads whole under /s/<P>/: its
// page, its script, the API calls the script makes (search and facets), the
// go-back page, the message links go-back and the search results build, and the
// way back to the search page — every one reached with the session cookie, none
// outside the prefix, none locked or missing [CC17].
func TestReaderLoadsUnderThePrefix(t *testing.T) {
	a := newGBArchive(t)
	a.add("store\x00mid:alpha", "Alpha", "store/Inbox/alpha.html", "Inbox", "Inbox", true, gbD1, gbD2)
	a.save()
	s := localweb.NewSession()
	for _, entry := range []string{"", "goback"} {
		loaded := browse(t, "serve", New(a.dir, a.ix), s, entry)
		assure.Reached(t, loaded, "URLs the reader loaded from "+entry)
		if bad := localwebtest.Check(loaded, s.Prefix()); len(bad) != 0 {
			t.Errorf("entry %q: %s", entry, strings.Join(bad, "; "))
		}
		for _, want := range []string{"app.js", "api/search", "api/facets", "goback", "files/store/Inbox/alpha.html"} {
			if entry == "goback" && (want == "app.js" || want == "api/search" || want == "api/facets") {
				continue
			}
			if !loadedHas(loaded, want) {
				t.Errorf("entry %q: %s was not loaded; loaded %v", entry, want, loaded)
			}
		}
	}
	// What app.js builds at run time for a search hit is files/ + the hit's path;
	// that URL answers under the prefix too.
	loaded := browse(t, "serve", New(a.dir, a.ix), s, "files/store/Inbox/alpha.html")
	if !loadedHas(loaded, "files/store/Inbox/alpha.html") {
		t.Errorf("a message page does not load under the prefix: %v", loaded)
	}
}

// covers: MA-372, R23, X9
// The setup wizard, as `setup` serves it, loads whole under /s/<P>/: its page, its
// script and the endpoint the script posts to (reached, answering 405 to a GET) —
// the wizard dies under a prefix if its script and API call are absolute [CC17].
func TestSetupLoadsUnderThePrefix(t *testing.T) {
	h, _, _ := newSetup(t)
	s := localweb.NewSession()
	loaded := browse(t, "setup", h, s, "")
	assure.Reached(t, loaded, "URLs the setup wizard loaded")
	if bad := localwebtest.Check(loaded, s.Prefix()); len(bad) != 0 {
		t.Errorf("%s", strings.Join(bad, "; "))
	}
	if !loadedHas(loaded, "setup.js") {
		t.Errorf("setup.js was not loaded: %v", loaded)
	}
	reached := false
	for _, l := range loaded {
		if strings.HasSuffix(l.URL, "api/setup") && l.Status == http.StatusMethodNotAllowed {
			reached = true
		}
	}
	if !reached {
		t.Errorf("the wizard's api/setup endpoint was not reached under the prefix: %v", loaded)
	}
}
