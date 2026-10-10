package localweb

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// rawExchange sends one raw request to addr and returns the parsed response.
func rawExchange(t *testing.T, addr, request string) *http.Response {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte(request)); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

var policyHeaders = []string{"Content-Security-Policy", "Cache-Control", "Cross-Origin-Resource-Policy", "X-Content-Type-Options", "Referrer-Policy"}

func missingPolicy(resp *http.Response) []string {
	var miss []string
	for _, h := range policyHeaders {
		if resp.Header.Get(h) == "" {
			miss = append(miss, h)
		}
	}
	return miss
}

// guardedServer serves Wrap(...) the way a production binary does: over a real
// listener, with the server options the binaries set.
func guardedServer(t *testing.T, disableOptions bool) string {
	t.Helper()
	inner := &countingInner{}
	srv := httptest.NewUnstartedServer(Wrap(Surface{Name: "serve", Listener: loopbackAt(8099)}, inner))
	srv.Config.DisableGeneralOptionsHandler = disableOptions
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

// covers: MA-371, MA-378, R23, R19, S43
// `OPTIONS *` is answered by net/http itself, before any handler, unless the
// server sets DisableGeneralOptionsHandler (which every production server does:
// the structural test asserts it). With it set, the request reaches the guard: a
// rebinding Host is refused 403 with the policy headers, a loopback Host passes.
// Read over a real listener: a handler-level recorder never sees net/http's own
// answer. (Adversarial pass AC8, AO1.)
func TestOptionsStarReachesTheGuard(t *testing.T) {
	addr := guardedServer(t, true)
	port := addr[strings.LastIndexByte(addr, ':')+1:]
	// Positive twin: a loopback Host passes through to the surface.
	ok := rawExchange(t, addr, "OPTIONS * HTTP/1.1\r\nHost: 127.0.0.1:"+port+"\r\nConnection: close\r\n\r\n")
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("OPTIONS * with a loopback Host: status %d, want the surface's 200", ok.StatusCode)
	}
	if miss := missingPolicy(ok); len(miss) > 0 {
		t.Errorf("OPTIONS * with a loopback Host: response lacks %v", miss)
	}
	bad := rawExchange(t, addr, "OPTIONS * HTTP/1.1\r\nHost: evil.example:"+port+"\r\nConnection: close\r\n\r\n")
	assure.Reached(t, bad.StatusCode, "status of a rebinding OPTIONS *")
	if bad.StatusCode != http.StatusForbidden {
		t.Errorf("OPTIONS * with Host evil.example: status %d, want 403", bad.StatusCode)
	}
	if miss := missingPolicy(bad); len(miss) > 0 {
		t.Errorf("OPTIONS * with Host evil.example: response lacks %v", miss)
	}
}

// covers: MA-371, MA-378, R23, R19, S43
// OPEN GAP (adversarial pass AC8, AO2; escalated, not fixed here): a request net/http
// rejects while parsing — a bad path escape, a malformed Host, oversized headers — is
// answered before any handler runs, with none of the policy headers. The design's
// "every response" cannot be delivered through http.Server's handler hook; it needs an
// owner decision (a listener-level wrapper, or narrowing the wording to handler answers).
// When the guarantee holds this test passes: REMOVE the skip so it becomes a guard.
func TestParseErrorAnswersCarryThePolicyHeaders(t *testing.T) {
	t.Skip("open: net/http's own 400 answers carry no policy headers (R19, R23, MA-378 'every response'; escalated, AC8)")
	addr := guardedServer(t, true)
	for _, req := range []string{
		"GET /%zz HTTP/1.1\r\nHost: 127.0.0.1:1\r\nConnection: close\r\n\r\n",
		"GET / HTTP/1.1\r\nHost: a b\r\nConnection: close\r\n\r\n",
	} {
		resp := rawExchange(t, addr, req)
		if miss := missingPolicy(resp); len(miss) > 0 {
			t.Errorf("%q: %d answer lacks %v", strings.SplitN(req, "\r\n", 2)[0], resp.StatusCode, miss)
		}
	}
}
