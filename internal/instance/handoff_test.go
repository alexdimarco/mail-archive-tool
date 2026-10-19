package instance

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/localweb"
)

// rig is an instance's handoff behind its own listener, over a real session: its
// browser opener is a recorder that can redeem the link (a browser that arrives),
// fail, or be ignored (a browser that never does).
type rig struct {
	h       *Handoff
	s       *localweb.Session
	srv     *httptest.Server
	addr    string
	key     []byte
	opened  atomic.Int32
	openErr error
	arrive  bool // the "browser" redeems the link it is given
	clock   *testClock
	links   []string
	mu      sync.Mutex
}

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newRig(t *testing.T) *rig {
	t.Helper()
	_, key := NewKey()
	g := &rig{s: localweb.NewSession(), key: key, arrive: true, clock: &testClock{t: time.Date(2026, 10, 10, 14, 0, 0, 0, time.UTC)}}
	g.h = &Handoff{Key: key, Wait: 2 * time.Second, Now: g.clock.Now}
	g.h.Mint = func(ttl time.Duration) (string, string) {
		tok := g.s.Mint(ttl)
		return g.srv.URL + localweb.OpenPath(tok), tok
	}
	g.h.Redeemed = g.s.WaitRedeemed
	g.h.Open = func(link string) error {
		g.opened.Add(1)
		g.mu.Lock()
		g.links = append(g.links, link)
		g.mu.Unlock()
		if g.openErr != nil {
			return g.openErr
		}
		if g.arrive {
			go http.Get(link)
		}
		return nil
	}
	g.srv = httptest.NewServer(localweb.Wrap(localweb.Surface{Name: "dashboard", Session: g.s, Handoff: g.h}, http.NotFoundHandler()))
	t.Cleanup(g.srv.Close)
	g.addr = strings.TrimPrefix(g.srv.URL, "http://")
	return g
}

// covers: MA-376, MA-373, R23
// The handoff is a challenge-response proven in both directions with the
// instance's key K: a launch that holds K makes the instance open its own browser
// once and learns the link was redeemed; a launch whose opener errors, or whose
// browser never arrives, is given a fresh 10-minute link through the dialog seam
// instead; a quiet caller (`-no-browser`, `-copy-link`, `serve -link`) gets a
// link and no browser; a second handoff inside a second is answered "busy"
// (MAC-covered) and counts as success [CC20, CC21, CC18].
func TestHandoffOpensTheInstancesOwnBrowserOnce(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()

	rep, err := Call(ctx, g.addr, g.key, false)
	if err != nil || !rep.Opened || rep.Link != "" || rep.Busy {
		t.Fatalf("a launch that holds K: %+v, %v; want Opened with no link", rep, err)
	}
	if g.opened.Load() != 1 {
		t.Errorf("the instance's browser opener was called %d times, want once", g.opened.Load())
	}
	if n, _ := g.s.Opened(); n != 1 {
		t.Errorf("sessions opened = %d, want 1", n)
	}

	// Within the same second: busy, and the browser is not opened again.
	rep, err = Call(ctx, g.addr, g.key, false)
	if err != nil || !rep.Busy || rep.Opened {
		t.Fatalf("a second handoff within a second: %+v, %v; want busy", rep, err)
	}
	if g.opened.Load() != 1 {
		t.Errorf("a busy handoff opened the browser again")
	}

	// An opener that errors: not opened, a fresh link comes back, MAC-covered.
	g.clock.Advance(2 * time.Second)
	g.openErr = errors.New("no default browser")
	rep, err = Call(ctx, g.addr, g.key, false)
	if err != nil || rep.Opened || !strings.HasPrefix(rep.Link, g.srv.URL+"/_open?t=") {
		t.Fatalf("an opener that errors: %+v, %v; want not opened with a link", rep, err)
	}
	if _, err := http.Get(rep.Link); err != nil { // the link works: a user pastes it
		t.Fatal(err)
	}
	if n, _ := g.s.Opened(); n != 2 {
		t.Errorf("the fresh link did not redeem: sessions opened = %d, want 2", n)
	}

	// A browser that never arrives within the wait: not opened, a link.
	g.clock.Advance(2 * time.Second)
	g.openErr, g.arrive = nil, false
	g.h.Wait = 100 * time.Millisecond
	rep, err = Call(ctx, g.addr, g.key, false)
	if err != nil || rep.Opened || rep.Link == "" {
		t.Fatalf("a browser that never arrives: %+v, %v; want not opened with a link", rep, err)
	}

	// Quiet: a link, no browser.
	g.clock.Advance(2 * time.Second)
	before := g.opened.Load()
	rep, err = Call(ctx, g.addr, g.key, true)
	if err != nil || rep.Opened || rep.Link == "" || g.opened.Load() != before {
		t.Fatalf("a quiet handoff: %+v, %v (opener calls %d -> %d); want a link and no browser", rep, err, before, g.opened.Load())
	}
}

// covers: MA-376, MA-373, R23
// A caller without the key gets nothing, and a client is never fooled by a
// listener: a wrong key is refused 403 and opens nothing; a squatter that answers
// 200 with a well-formed reply but the wrong proof, one that replays a valid reply
// meant for another nonce, and one that swaps the link in a valid reply are all
// ErrBadProof — the client opens nothing at that address [CC20, CC21, P8, F4].
func TestHandoffRefusesWhoCannotProve(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()

	_, wrong := NewKey()
	if _, err := Call(ctx, g.addr, wrong, false); !errors.Is(err, ErrBadProof) {
		t.Errorf("a caller with the wrong key: %v, want ErrBadProof", err)
	}
	if g.opened.Load() != 0 {
		t.Errorf("the wrong key made the instance open a browser")
	}

	// Raw requests: GET, an oversized body, junk, a bad nonce, a bad proof.
	post := func(body string) int {
		resp, err := http.Post(g.srv.URL+"/_handoff", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if resp, _ := http.Get(g.srv.URL + "/_handoff"); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /_handoff: %d, want 405", resp.StatusCode)
	}
	n := strings.Repeat("ab", 32)
	if c := post(`{"n":"` + n + `","p":"` + MAC(g.key, "handoff-req", n) + `","pad":"` + strings.Repeat("x", 2<<10) + `"}`); c != http.StatusBadRequest {
		t.Errorf("an oversized body: %d, want 400", c)
	}
	for name, body := range map[string]string{"junk": `not json`, "short nonce": `{"n":"abcd","p":"x"}`, "no proof": `{"n":"` + n + `"}`, "bad proof": `{"n":"` + n + `","p":"` + strings.Repeat("0", 64) + `"}`} {
		if c := post(body); c != http.StatusBadRequest && c != http.StatusForbidden {
			t.Errorf("%s: %d, want 400 or 403", name, c)
		}
	}
	if g.opened.Load() != 0 {
		t.Errorf("a refused request made the instance open a browser")
	}

	// Squatters.
	squat := func(reply func(n string) any) string {
		sq := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req struct{ N string }
			json.NewDecoder(r.Body).Decode(&req)
			json.NewEncoder(w).Encode(reply(req.N))
		}))
		t.Cleanup(sq.Close)
		return strings.TrimPrefix(sq.URL, "http://")
	}
	otherN := strings.Repeat("cd", 32)
	for name, addr := range map[string]string{
		"wrong proof": squat(func(string) any { return map[string]any{"opened": true, "p": strings.Repeat("0", 64)} }),
		"no proof":    squat(func(string) any { return map[string]any{"opened": true} }),
		"replayed reply": squat(func(string) any {
			return map[string]any{"opened": true, "p": MAC(g.key, "handoff-ok", otherN, "true", "")}
		}),
		"busy, unproven": squat(func(string) any { return map[string]any{"busy": true, "p": "0"} }),
		"swapped link": squat(func(n string) any {
			return map[string]any{"opened": false, "link": "http://127.0.0.1:1/_open?t=EVIL", "p": MAC(g.key, "handoff-ok", n, "false", "http://127.0.0.1:1/_open?t=GOOD")}
		}),
		"opened flipped": squat(func(n string) any {
			return map[string]any{"opened": true, "p": MAC(g.key, "handoff-ok", n, "false", "")}
		}),
		"not a json reply": squat(func(string) any { return "hello" }),
	} {
		if _, err := Call(ctx, addr, g.key, false); !errors.Is(err, ErrBadProof) {
			t.Errorf("a squatter (%s): %v, want ErrBadProof", name, err)
		}
	}
	// Nothing is listening at all.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().String()
	ln.Close()
	if _, err := Call(ctx, dead, g.key, false); err == nil || errors.Is(err, ErrBadProof) {
		t.Errorf("nothing listening: %v, want a connection error", err)
	}
}

// covers: MA-376, R12
// A failed handshake is retried for a bounded time while the lock is held [CC21]:
// an instance that starts listening after the first try is reached, a lock that is
// no longer held ends the retrying at once, and the window bounds it; a quiet
// caller waits out a busy answer for its link.
func TestReachRetriesWhileTheLockIsHeld(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()

	// The instance comes up on addr 300 ms in.
	go func() {
		time.Sleep(300 * time.Millisecond)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return
		}
		srv := &http.Server{Handler: localweb.Wrap(localweb.Surface{Name: "dashboard", Session: g.s, Handoff: g.h}, http.NotFoundHandler())}
		t.Cleanup(func() { srv.Close() })
		srv.Serve(l)
	}()
	start := time.Now()
	rep, err := Reach(ctx, addr, g.key, true, 10*time.Second, func() bool { return true })
	if err != nil || rep.Link == "" {
		t.Fatalf("Reach did not find the instance that came up late: %+v, %v", rep, err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("Reach took %v", time.Since(start))
	}

	// The lock is free: no point retrying.
	ln, _ = net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().String()
	ln.Close()
	start = time.Now()
	_, err = Reach(ctx, dead, g.key, false, 10*time.Second, func() bool { return false })
	assure.Reached(t, errString(err), "a refusal when the lock is not held")
	if time.Since(start) > 2*time.Second {
		t.Errorf("Reach retried for %v though the lock was free", time.Since(start))
	}
	// The window bounds the retrying.
	start = time.Now()
	_, err = Reach(ctx, dead, g.key, false, 700*time.Millisecond, func() bool { return true })
	if err == nil || time.Since(start) > 3*time.Second {
		t.Errorf("Reach with a 700 ms window: %v after %v", err, time.Since(start))
	}
}
