package desktop

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/localweb"
	"mail-archive-tool/internal/localweb/localwebtest"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/server"
)

func loopback(port int) net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port} }


// sessionCookie redeems a fresh one-time link of s on h and returns the cookie a
// browser would then hold, so a fixture can read what only an admitted browser sees.
func sessionCookie(t *testing.T, h http.Handler, s *localweb.Session, host string) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, localweb.OpenPath(s.Mint(localweb.LinkTTL)), nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == localweb.SessionCookie {
			return c
		}
	}
	t.Fatalf("redeeming a launch link on %s gave no session cookie (status %d)", host, rec.Code)
	return nil
}

// dashboardFixture is the dashboard as the binary serves it, configured for a
// tenant (identity data on the page) but with no archive location, so no probe
// reaches the OS scheduler or an archive. It returns the CSRF token the page
// carries — read inside the session, under /s/<P>/, as only an admitted browser
// can — and the settings file a state-changing request would write.
func dashboardFixture(t *testing.T) (http.Handler, string, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "contoso-tenant-0451", ClientID: "app-client-7788", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "desktop-settings.json")
	sess := localweb.NewSession()
	h := dashboardHandler(Config{ConfigPath: cfgPath, SettingsPath: settings, TokenCachePath: filepath.Join(dir, "tok.json")}, loopback(8097), sess, nil)
	req := httptest.NewRequest(http.MethodGet, sess.Prefix(), nil)
	req.Host = "127.0.0.1:8097"
	req.AddCookie(sessionCookie(t, h, sess, req.Host))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	m := dashCSRFRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("the dashboard page carries no CSRF token (status %d)", rec.Code)
	}
	return h, m[1], settings
}

// readerFixture is an archive with one indexed message, for the embedded reader.
func readerFixture(t *testing.T) (string, http.Handler) {
	t.Helper()
	out := t.TempDir()
	ix, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	m := &model.Message{Subject: "Quarterly merger terms", SenderName: "Bob", SenderEmail: "bob@example.com",
		Received: time.Date(2025, 7, 3, 9, 0, 0, 0, time.UTC), PlainBody: "the merger terms are attached"}
	if err := ix.Add("store", []string{"Inbox"}, m, "store/Inbox/merger.html", "Inbox\x00id1"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	rm := ReaderHandler(Config{Out: out})
	// Release the reader's index before the TempDir cleanup (Windows cannot
	// delete an open file); this Cleanup runs before TempDir's.
	t.Cleanup(func() { rm.(io.Closer).Close() })
	return out, rm
}

// covers: MA-371, R19, R23, S43
// The dashboard and the embedded reader, as the binary builds them, check the
// Host header first on every registered route and on GET, POST and HEAD: a
// rebinding page's name, an empty Host, a trailing dot or userinfo gets the 403
// refusal with none of the page's identity data, the archive's subject or path,
// or the dashboard's CSRF token, and changes nothing; every loopback name passes
// on any port, a forwarded one included [CC18].
func TestDashboardAndReaderRefuseForeignHost(t *testing.T) {
	hostile := func(p string) []string {
		return []string{"attacker.example:" + p, "", "127.0.0.1.:" + p, "127.0.0.1.", "user@127.0.0.1:" + p}
	}
	passed := func(p string) []string {
		return []string{"127.0.0.1:" + p, "localhost:" + p, "[::1]:" + p, "127.0.0.1:9000"}
	}

	dash, csrf, settings := dashboardFixture(t)
	probed := localwebtest.Probe{
		Surface:   "dashboard",
		Refused:   hostile("8097"),
		Passed:    passed("8097"),
		Forbid:    []string{csrf, "contoso-tenant-0451", "app-client-7788"},
		Unchanged: func() bool { _, err := os.Stat(settings); return os.IsNotExist(err) },
	}.Run(t, dash)
	assure.Reached(t, probed, "dashboard routes probed")

	out, rm := readerFixture(t)
	probed = localwebtest.Probe{
		Surface: "reader",
		Refused: hostile("8099"),
		Passed:  passed("8099"),
		Forbid:  []string{"Quarterly merger", "store/Inbox", out},
	}.Run(t, readerHandler(rm, loopback(8099), localweb.NewSession()))
	assure.Reached(t, probed, "reader routes probed")
}

// covers: MA-378, R23
// MA-378's other half for the desktop, closed by sessions (P7, slice LC-C2): with
// a loopback Host but no session, every route the dashboard and the reader
// register must refuse on GET, POST and HEAD — 401, the locked page or a JSON 401
// — with none of the dashboard's CSRF token or identity data and none of the
// archive's subject or path. This was the strict encoder that skipped while any
// such request was still answered; it failed the day none was, its skip was
// removed as it said, and it stays as the permanent guard. The handlers are
// built directly, with no request to read a token first.
func TestDashboardAndReaderRefuseWithoutSession(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "contoso-tenant-0451", ClientID: "app-client-7788", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	dash := dashboardHandler(Config{ConfigPath: cfgPath, SettingsPath: filepath.Join(dir, "desktop-settings.json"), TokenCachePath: filepath.Join(dir, "tok.json")}, loopback(8097), localweb.NewSession(), nil)
	out, rm := readerFixture(t)

	admitted := localwebtest.Probe{Surface: "dashboard", Forbid: []string{`name="csrf"`, "contoso-tenant-0451", "app-client-7788"}}.
		Admitted(t, dash, "127.0.0.1:8097")
	admitted = append(admitted, localwebtest.Probe{Surface: "reader", Forbid: []string{"Quarterly merger", "store/Inbox", out}}.
		Admitted(t, readerHandler(rm, loopback(8099), localweb.NewSession()), "127.0.0.1:8099")...)
	if len(admitted) != 0 {
		t.Fatalf("MA-378 / R23: %d loopback-Host request(s) with no session were answered, e.g. %s",
			len(admitted), strings.Join(admitted[:min(3, len(admitted))], "; "))
	}
}

// covers: MA-378, R23, R19
// Every route the dashboard and the reader register is served only through the
// guard: the binary's constructors return a localweb.Wrap handler, the routes it
// names are exactly the ones the inner handler registered, and each is probed by
// name with a hostile Host — refused on GET, POST and HEAD — and a loopback Host,
// which gets through; every response carries the policy headers.
func TestDashboardAndReaderServeOnlyThroughTheGuard(t *testing.T) {
	dash, _, _ := dashboardFixture(t)
	out, rm := readerFixture(t)
	for _, c := range []struct {
		name       string
		port       string
		served     http.Handler
		registered []string
	}{
		{"dashboard", "8097", dash, localweb.RoutesOf(DashboardHandler(Config{}))},
		{"reader", "8099", readerHandler(rm, loopback(8099), localweb.NewSession()), localweb.RoutesOf(server.New(out, nil))},
	} {
		probed := localwebtest.Probe{
			Surface: c.name,
			Refused: []string{"attacker.example:" + c.port},
			Passed:  []string{"127.0.0.1:" + c.port},
		}.Run(t, c.served)
		assure.Reached(t, probed, c.name+" routes probed")
		if !reflect.DeepEqual(probed, c.registered) {
			t.Errorf("%s: probed %q, but the handler registered %q", c.name, probed, c.registered)
		}
	}
}
