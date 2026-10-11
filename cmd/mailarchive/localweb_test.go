package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/localweb"
	"mail-archive-tool/internal/localweb/localwebtest"
	"mail-archive-tool/internal/server"
)

func loopback(port int) net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port} }

// readerFixture is a served archive: buildSearchFixture's index (subjects such as
// "Acme invoice #4471") plus a root page carrying that subject, so a passing
// request really reads archive content and a refused one must not.
func readerFixture(t *testing.T) (string, *index.Index) {
	t.Helper()
	out := buildSearchFixture(t)
	if err := os.WriteFile(filepath.Join(out, "index.html"), []byte("<p>Acme invoice #4471</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
	ix, err := index.OpenReadonly(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	return out, ix
}

var setupCSRFRe = regexp.MustCompile(`name="csrf" content="([0-9a-f]+)"`)

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

// setupFixture is the setup wizard as `setup` serves it, with its CSRF token read
// through a loopback Host the way the page delivers it: inside the session, under
// /s/<P>/, which only a browser that redeemed a launch link can reach.
func setupFixture(t *testing.T) (http.Handler, string, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	sess := localweb.NewSession()
	h := setupHandler(cfgPath, graphconfig.NewFileSecretStore(filepath.Join(dir, "secrets")), loopback(8097), sess)
	req := httptest.NewRequest(http.MethodGet, sess.Prefix(), nil)
	req.Host = "127.0.0.1:8097"
	req.AddCookie(sessionCookie(t, h, sess, req.Host))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	m := setupCSRFRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("the setup page carries no CSRF token (status %d)", rec.Code)
	}
	return h, cfgPath, m[1]
}

// hostileHosts are MA-371's refused Host forms for port p: a rebinding page's
// own name, an empty Host, a trailing dot (with and without a port), userinfo.
func hostileHosts(p string) []string {
	return []string{"attacker.example:" + p, "", "127.0.0.1.:" + p, "127.0.0.1.", "user@127.0.0.1:" + p}
}

// loopbackHosts are MA-371's positive twins for port p, a forwarded port included.
func loopbackHosts(p string) []string {
	return []string{"127.0.0.1:" + p, "localhost:" + p, "[::1]:" + p, "127.0.0.1:9000"}
}

// covers: MA-371, R19, R23, S43
// `serve` and `setup`, as the binary builds them, check the Host header first on
// every registered route and on GET, POST and HEAD: a rebinding page's name, an
// empty Host, a trailing dot or userinfo gets the 403 refusal with none of the
// archive's subjects or paths and none of setup's CSRF token, and changes
// nothing; every loopback name passes on any port, a forwarded one included
// [CC18]. A serve bound to 0.0.0.0 also accepts this machine's interface IP
// literals and its -allow-host names, and still refuses every other name.
func TestServeAndSetupRefuseForeignHost(t *testing.T) {
	out, ix := readerFixture(t)
	serve := serveHandler(out, ix, loopback(8099), nil, localweb.NewSession(), nil)
	probed := localwebtest.Probe{
		Surface: "serve",
		Refused: hostileHosts("8099"),
		Passed:  loopbackHosts("8099"),
		Forbid:  []string{"Acme invoice", "store/Inbox", out},
	}.Run(t, serve)
	assure.Reached(t, probed, "serve routes probed")

	setup, cfgPath, csrf := setupFixture(t)
	probed = localwebtest.Probe{
		Surface:   "setup",
		Refused:   hostileHosts("8097"),
		Passed:    loopbackHosts("8097"),
		Forbid:    []string{csrf, cfgPath},
		Unchanged: func() bool { _, err := os.Stat(cfgPath); return os.IsNotExist(err) },
	}.Run(t, setup)
	assure.Reached(t, probed, "setup routes probed")

	// A serve bound to every interface: its interface IP literals and the
	// -allow-host names pass, every other name is still refused.
	passed := []string{"nas.example:8099", "NAS.example:9000", "127.0.0.1:8099"}
	if ip := interfaceIPv4(); ip != "" {
		passed = append(passed, ip+":8099")
	} else {
		t.Log("this machine has no non-loopback IPv4 interface; the interface-literal case is not shown")
	}
	lan := serveHandler(out, ix, &net.TCPAddr{IP: net.IPv4zero, Port: 8099}, []string{"nas.example"}, localweb.NewSession(), nil)
	localwebtest.Probe{
		Surface: "serve",
		Refused: []string{"attacker.example:8099", "nas.example.:8099", "192.0.2.1:8099"},
		Passed:  passed,
		Forbid:  []string{"Acme invoice", out},
	}.Run(t, lan)

	// On a loopback bind the -allow-host names widen nothing.
	loop := serveHandler(out, ix, loopback(8099), []string{"nas.example"}, localweb.NewSession(), nil)
	localwebtest.Probe{Surface: "serve", Refused: []string{"nas.example:8099"}, Forbid: []string{"Acme invoice"}}.Run(t, loop)
}

// interfaceIPv4 is a non-loopback IPv4 address of this machine, or "".
func interfaceIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && ipn.IP.To4() != nil {
			return ipn.IP.To4().String()
		}
	}
	return ""
}

// covers: MA-371, R12, S43
// -allow-host takes a host name or IP address only: a value with a port, a
// scheme or a path is refused naming the flag and the value, before any archive
// is opened, and a valid name is accepted.
func TestServeAllowHostIsAHostName(t *testing.T) {
	for _, bad := range []string{"nas.example:8099", "http://nas.example", "nas.example/x", "user@nas.example", "nas.example.", "fe80::1%en0"} {
		code, stderr := runCLI("serve", "-out", t.TempDir(), "-allow-host", bad)
		assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-allow-host", bad, "without a port"))
	}
	got, err := allowHostNames([]string{"NAS.example", "192.168.1.10", "2001:db8::1", "[2001:DB8::2]"})
	if err != nil {
		t.Fatalf("valid -allow-host values refused: %v", err)
	}
	if want := []string{"nas.example", "192.168.1.10", "[2001:db8::1]", "[2001:db8::2]"}; !reflect.DeepEqual(got, want) {
		t.Errorf("allowHostNames = %q, want %q", got, want)
	}
}

// covers: MA-378, R23, R19
// Every route `serve` and `setup` register is served only through the guard: the
// binary's constructors return a localweb.Wrap handler, the routes it names are
// exactly the ones the inner handler registered, and each is probed by name with
// a hostile Host — refused on GET, POST and HEAD — and with a loopback Host,
// which gets through; every response carries the policy headers.
func TestServeAndSetupServeOnlyThroughTheGuard(t *testing.T) {
	out, ix := readerFixture(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	store := graphconfig.NewFileSecretStore(filepath.Join(dir, "secrets"))
	for _, c := range []struct {
		name       string
		port       string
		served     http.Handler
		registered []string
	}{
		{"serve", "8099", serveHandler(out, ix, loopback(8099), nil, localweb.NewSession(), nil), localweb.RoutesOf(server.New(out, ix))},
		{"setup", "8097", setupHandler(cfgPath, store, loopback(8097), localweb.NewSession()), localweb.RoutesOf(server.SetupHandler(cfgPath, store))},
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

// covers: MA-378, R23
// MA-378's other half, closed by sessions (P7, slice LC-C2): with a loopback
// Host but no session, every route `serve` and `setup` register must refuse on
// GET, POST and HEAD — 401, the locked page or a JSON 401 — with none of the
// archive's subjects or paths, nor setup's CSRF token or config path. This was
// the strict encoder that skipped while any such request was still answered; it
// failed the day none was, its skip was removed as it said, and it stays as the
// permanent guard. The desktop's surfaces have their own
// (TestDashboardAndReaderRefuseWithoutSession).
func TestEveryRouteRefusesWithoutSession(t *testing.T) {
	out, ix := readerFixture(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	setup := setupHandler(cfgPath, graphconfig.NewFileSecretStore(filepath.Join(dir, "secrets")), loopback(8097), localweb.NewSession())

	admitted := localwebtest.Probe{Surface: "serve", Forbid: []string{"Acme invoice", "store/Inbox", out}}.
		Admitted(t, serveHandler(out, ix, loopback(8099), nil, localweb.NewSession(), nil), "127.0.0.1:8099")
	admitted = append(admitted, localwebtest.Probe{Surface: "setup", Forbid: []string{`name="csrf"`, cfgPath}}.
		Admitted(t, setup, "127.0.0.1:8097")...)
	if len(admitted) != 0 {
		t.Fatalf("MA-378 / R23: %d loopback-Host request(s) with no session were answered, e.g. %s",
			len(admitted), strings.Join(admitted[:min(3, len(admitted))], "; "))
	}
}
