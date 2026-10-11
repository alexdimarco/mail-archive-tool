package desktop

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/localweb"
	"mail-archive-tool/internal/localweb/localwebtest"
)

// covers: MA-372, R23, X9
// No HTML or script the dashboard serves names a same-origin absolute URL: its
// page (links to the overview, the script) and its script (every API call) are
// relative, so they resolve under /s/<P>/ [CC17]. The page is read configured, so
// its Microsoft 365 block and its reader links are in it.
func TestDashboardPagesHaveNoSameOriginAbsoluteURL(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "contoso-tenant-0451", ClientID: "app-client-7788", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	h := DashboardHandler(Config{ConfigPath: cfgPath, SettingsPath: filepath.Join(dir, "s.json"), TokenCachePath: filepath.Join(dir, "t.json"), ReaderURL: "http://127.0.0.1:1/s/x/"})
	for _, p := range []string{"/", "/dashboard.js"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.Host = "127.0.0.1:8097"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assure.Reached(t, rec.Body.String(), "the dashboard's "+p)
		if bad := localwebtest.SameOriginAbsoluteURLs(rec.Body.String()); len(bad) != 0 {
			t.Errorf("%s names same-origin absolute URLs %q; they must be relative to the page", p, bad)
		}
	}
}

// covers: MA-372, R23, X9
// The dashboard loads whole under /s/<P>/ — its page, its script, every API call
// the script makes — and its "Open archive" and "Go back in time" links reach the
// reader on the reader's own port with the same cookie, because the two surfaces
// share one session and a cookie for host 127.0.0.1 goes to every port [CC17].
func TestDashboardAndItsReaderLinkLoadUnderThePrefix(t *testing.T) {
	out := t.TempDir()
	ix, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	ix.Close()
	s := localweb.NewSession()
	rm := ReaderHandler(Config{Out: out})
	t.Cleanup(func() { rm.(io.Closer).Close() })
	reader := httptest.NewServer(localweb.Wrap(localweb.Surface{Name: "reader", Session: s}, rm))
	t.Cleanup(reader.Close)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "contoso-tenant-0451", ClientID: "app-client-7788", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	dash := httptest.NewServer(localweb.Wrap(localweb.Surface{Name: "dashboard", Session: s},
		DashboardHandler(Config{ConfigPath: cfgPath, SettingsPath: filepath.Join(dir, "s.json"), TokenCachePath: filepath.Join(dir, "t.json"), ReaderURL: reader.URL + s.Prefix()})))
	t.Cleanup(dash.Close)

	loaded := localwebtest.Browse(t, dash.URL, s, "")
	assure.Reached(t, loaded, "URLs the dashboard loaded")
	if bad := localwebtest.Check(loaded, s.Prefix()); len(bad) != 0 {
		t.Errorf("%s", strings.Join(bad, "; "))
	}
	has := func(prefix, suffix string) bool {
		for _, l := range loaded {
			if strings.HasPrefix(l.URL, prefix) && strings.HasSuffix(l.URL, suffix) && l.Status == http.StatusOK {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"dashboard.js", "api/activity", "api/health", "api/schedule-state"} {
		if !has(dash.URL, want) {
			t.Errorf("%s was not loaded from the dashboard's port: %v", want, loaded)
		}
	}
	if !has(reader.URL, s.Prefix()) || !has(reader.URL, "goback") {
		t.Errorf("the dashboard's reader links did not load on the reader's port: %v", loaded)
	}
}
