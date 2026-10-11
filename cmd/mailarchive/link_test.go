package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/localweb"
	"mail-archive-tool/internal/localweb/localwebtest"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
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

// countingReader counts the reads made of it: a backgrounded serve must make none.
type countingReader struct {
	r     io.Reader
	calls atomic.Int32
}

func (c *countingReader) Read(p []byte) (int, error) { c.calls.Add(1); return c.r.Read(p) }

// rigOpts says what the surface's surroundings are: which streams are terminals,
// whether the process is in the foreground, whether there is a display, and
// whether the browser opener fails.
type rigOpts struct {
	stdoutTTY, stdinTTY, foreground, display bool
	openErr                                  error
}

// linkRig runs `serve` or `setup` in-process with every surrounding injected: no
// browser is ever started, no terminal is needed, the per-account state directory
// is a temp dir and the clock is the test's.
type linkRig struct {
	t              *testing.T
	out, state     string
	stdout, stderr *syncBuf
	clock          *testClock
	mu             sync.Mutex
	opened         []string
	origin         string
	stdinW         *io.PipeWriter
	stdinR         *countingReader
	fg             atomic.Bool
	cancel         context.CancelFunc
	done           chan error
	env            *linkEnv
}

func (g *linkRig) openedLinks() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.opened...)
}

var openLinkRe = regexp.MustCompile(`https?://[^\s]+/_open\?t=[A-Za-z0-9_-]+`)

// printed is every launch link on stdout, in order.
func (g *linkRig) printed() []string { return openLinkRe.FindAllString(g.stdout.String(), -1) }

// newEnv builds the injected environment; startSurface and the -link run share it.
func newLinkRig(t *testing.T, o rigOpts) *linkRig {
	t.Helper()
	g := &linkRig{t: t, stdout: &syncBuf{}, stderr: &syncBuf{}, state: t.TempDir(),
		clock: &testClock{t: time.Date(2026, 10, 10, 14, 0, 0, 0, time.UTC)}}
	stdoutF, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	stdinF, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stdoutF.Close(); stdinF.Close() })
	pr, pw := io.Pipe()
	g.stdinW = pw
	g.stdinR = &countingReader{r: pr}
	t.Cleanup(func() { pw.Close() })
	g.fg.Store(o.foreground)
	g.env = &linkEnv{
		Stdin: stdinF, StdinReader: g.stdinR, Stdout: g.stdout, StdoutFile: stdoutF, Stderr: g.stderr,
		Getenv: func(k string) string {
			if k == "DISPLAY" && o.display {
				return ":0"
			}
			return ""
		},
		GOOS: "linux",
		Open: func(u string) error {
			g.mu.Lock()
			g.opened = append(g.opened, u)
			g.mu.Unlock()
			return o.openErr
		},
		StateDir:   func() (string, error) { return g.state, nil },
		IsTerminal: func(f *os.File) bool { return (f == stdoutF && o.stdoutTTY) || (f == stdinF && o.stdinTTY) },
		Foreground: func(*os.File) bool { return g.fg.Load() },
		Now:        g.clock.Now,
	}
	return g
}

// startSurface runs run (serveMain or setupMain) with args until the test ends,
// and returns once it is serving.
func startSurface(t *testing.T, o rigOpts, run func(context.Context, []string, *linkEnv) error, out string, args ...string) *linkRig {
	t.Helper()
	g := newLinkRig(t, o)
	g.out = out
	ready := make(chan string, 1)
	g.env.Ready = func(origin string) { ready <- origin }
	ctx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	g.done = make(chan error, 1)
	go func() { g.done <- run(ctx, args, g.env) }()
	t.Cleanup(g.stop)
	select {
	case g.origin = <-ready:
	case err := <-g.done:
		t.Fatalf("the surface ended before it was serving: %v\nstdout: %s\nstderr: %s", err, g.stdout.String(), g.stderr.String())
	case <-time.After(15 * time.Second):
		t.Fatalf("the surface was not serving after 15 s\nstdout: %s", g.stdout.String())
	}
	return g
}

func (g *linkRig) stop() {
	if g.cancel == nil {
		return
	}
	g.cancel()
	select {
	case <-g.done:
	case <-time.After(10 * time.Second):
		g.t.Errorf("the surface did not stop within 10 s of its context ending")
	}
	g.cancel = nil
}

func serveArgs(out string, extra ...string) []string {
	return append([]string{"-out", out, "-addr", "127.0.0.1:0"}, extra...)
}

var noRedirects = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func fetch(t *testing.T, u string, cookies ...*http.Cookie) (int, string, []*http.Cookie) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := noRedirects.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, string(b), resp.Cookies()
}

// waitFor polls until cond holds or the deadline passes.
func waitFor(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// covers: MA-373, R23, X8
// `serve` opens the browser at its own link and, on a terminal, also prints a
// separate one, worded as the fallback: two tokens, each good for one redemption;
// every redemption is announced once on stderr with its running count; the
// served address is on stdout as always (X8) [CC5, CC19, CC24].
func TestServeOpensItsOwnLinkAndPrintsAnother(t *testing.T) {
	out := buildSearchFixture(t)
	g := startSurface(t, rigOpts{stdoutTTY: true, display: true, foreground: true}, serveMain, out, serveArgs(out)...)
	opened := g.openedLinks()
	if len(opened) != 1 || !strings.HasPrefix(opened[0], g.origin+"/_open?t=") {
		t.Fatalf("the browser opener was given %q, want one link on %s", opened, g.origin)
	}
	printed := g.printed()
	if len(printed) != 1 || printed[0] == opened[0] {
		t.Fatalf("printed links %q: want exactly one, and not the opener's %q (separate tokens)", printed, opened[0])
	}
	stdout := g.stdout.String()
	for _, want := range []string{"Search UI:  " + g.origin + "/", "If your browser did not open, use: " + printed[0], "Press Ctrl-C to stop."} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "get a link with") {
		t.Errorf("a terminal is told to run serve -link: %s", stdout)
	}
	for i, l := range []string{opened[0], printed[0]} {
		if code, _, _ := fetch(t, l); code != http.StatusOK {
			t.Errorf("link %d: status %d, want 200", i, code)
		}
	}
	if code, body, _ := fetch(t, printed[0]); code != http.StatusUnauthorized || !strings.Contains(body, "already been used") {
		t.Errorf("a printed link used twice: status %d, want the 401 used-link page", code)
	}
	notices := g.stderr.String()
	if !strings.Contains(notices, "browser session opened at ") || !strings.Contains(notices, "(1 this run)") || !strings.Contains(notices, "(2 this run)") {
		t.Errorf("stderr lacks one notice per redemption with its count: %q", notices)
	}
	if strings.Contains(notices, "_open?t=") || strings.Contains(stdout+notices, "mailarchive_session") {
		t.Errorf("a token or the cookie reached the output: %q", notices)
	}
}

// covers: MA-373, R23
// The opener's link lives about 60 s and the printed one 10 min [CC5]: past 61 s
// the opened link is "expired" and the printed one still works; past ten minutes
// the printed one is expired too.
func TestServeLinkLifetimes(t *testing.T) {
	out := buildSearchFixture(t)
	g := startSurface(t, rigOpts{stdoutTTY: true, display: true, foreground: true}, serveMain, out, serveArgs(out)...)
	opened, printed := g.openedLinks()[0], g.printed()[0]
	g.clock.Advance(61 * time.Second)
	if code, body, _ := fetch(t, opened); code != http.StatusUnauthorized || !strings.Contains(body, "has expired") {
		t.Errorf("the opener's link after 61 s: status %d, want the expiry page", code)
	}
	if code, _, _ := fetch(t, printed); code != http.StatusOK {
		t.Errorf("the printed link after 61 s: status %d, want 200", code)
	}
	g2 := startSurface(t, rigOpts{stdoutTTY: true, display: true, foreground: true}, serveMain, out, serveArgs(out)...)
	printed2 := g2.printed()[0]
	g2.clock.Advance(10*time.Minute + time.Second)
	if code, body, _ := fetch(t, printed2); code != http.StatusUnauthorized || !strings.Contains(body, "has expired") {
		t.Errorf("a printed link after 10 min: status %d, want the expiry page", code)
	}
}

// covers: MA-373, R23, X8
// A token is printed only to a terminal [CC5]: with stdout a journal, nohup.out
// or a CI log, `serve` prints the address and how to get a link
// (`mailarchive serve -link -out <folder>`) and no token anywhere in its output —
// though it still opens the browser, which is not a log.
func TestServeNeverPrintsATokenToANonTerminal(t *testing.T) {
	out := buildSearchFixture(t)
	g := startSurface(t, rigOpts{stdoutTTY: false, display: true}, serveMain, out, serveArgs(out)...)
	stdout := g.stdout.String()
	if strings.Contains(stdout, "_open") || strings.Contains(g.stderr.String(), "_open") || strings.Contains(stdout, "t=") {
		t.Errorf("a token reached a non-terminal's output: %q", stdout)
	}
	for _, want := range []string{"Search UI:  " + g.origin + "/", "get a link with: mailarchive serve -link -out " + out} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if len(g.openedLinks()) != 1 {
		t.Errorf("the browser was opened %d times, want once (a browser is not a log)", len(g.openedLinks()))
	}
}

// covers: MA-373, R23
// No browser is opened with -no-browser, without a display (a headless ssh
// session, where xdg-open could start a text browser [CC18]), or at all if the
// opener fails; on a terminal the link is then printed as the way in, not as a
// fallback.
func TestServeOpensNoBrowserWhenItMust(t *testing.T) {
	out := buildSearchFixture(t)
	for _, c := range []struct {
		name  string
		o     rigOpts
		extra []string
		opens int
	}{
		{"-no-browser", rigOpts{stdoutTTY: true, display: true}, []string{"-no-browser"}, 0},
		{"no display", rigOpts{stdoutTTY: true, display: false}, nil, 0},
		{"an opener that fails", rigOpts{stdoutTTY: true, display: true, openErr: io.ErrClosedPipe}, nil, 1},
	} {
		g := startSurface(t, c.o, serveMain, out, serveArgs(out, c.extra...)...)
		if n := len(g.openedLinks()); n != c.opens {
			t.Errorf("%s: the opener was called %d times, want %d", c.name, n, c.opens)
		}
		printed := g.printed()
		if len(printed) != 1 || !strings.Contains(g.stdout.String(), "Open this link in your browser: "+printed[0]) {
			t.Errorf("%s: stdout does not print the link as the way in:\n%s", c.name, g.stdout.String())
		}
		if strings.Contains(g.stdout.String(), "If your browser did not open") {
			t.Errorf("%s: the fallback wording is used though no browser was opened", c.name)
		}
		if code, _, _ := fetch(t, printed[0]); code != http.StatusOK {
			t.Errorf("%s: the printed link: status %d, want 200", c.name, code)
		}
		g.stop()
	}
}

// covers: MA-373, R23
// With stdin a terminal and the process in its foreground group, Enter prints a
// fresh link; a backgrounded process makes no read of stdin at all (a read would
// stop it with SIGTTIN) and takes the line once it is in the foreground again, as
// after `fg`; with no terminal on stdin nothing reads it [CC18].
func TestServeEnterPrintsAFreshLinkOnlyInTheForeground(t *testing.T) {
	out := buildSearchFixture(t)
	g := startSurface(t, rigOpts{stdoutTTY: true, stdinTTY: true, foreground: false, display: true}, serveMain, out, serveArgs(out)...)
	if !strings.Contains(g.stdout.String(), "Press Enter for a new link, Ctrl-C to stop.") {
		t.Errorf("the start-up text does not offer Enter:\n%s", g.stdout.String())
	}
	first := g.printed()
	go g.stdinW.Write([]byte("\n")) // the user pressed Enter while the process was in the background
	time.Sleep(700 * time.Millisecond)
	if n := g.stdinR.calls.Load(); n != 0 {
		t.Fatalf("a backgrounded process read stdin %d time(s); it must make none", n)
	}
	if len(g.printed()) != len(first) {
		t.Errorf("a link was printed while backgrounded:\n%s", g.stdout.String())
	}
	g.fg.Store(true) // fg
	waitFor(t, "the link after fg", 5*time.Second, func() bool { return len(g.printed()) == len(first)+1 })
	fresh := g.printed()[len(first)]
	for _, l := range first {
		if fresh == l {
			t.Errorf("the Enter link repeats an earlier link")
		}
	}
	if code, _, _ := fetch(t, fresh); code != http.StatusOK {
		t.Errorf("the Enter link: status %d, want 200", code)
	}

	noTTY := startSurface(t, rigOpts{stdoutTTY: true, stdinTTY: false, foreground: true, display: true}, serveMain, out, serveArgs(out)...)
	if strings.Contains(noTTY.stdout.String(), "Press Enter") || !strings.Contains(noTTY.stdout.String(), "Press Ctrl-C to stop.") {
		t.Errorf("without a terminal on stdin the text still offers Enter:\n%s", noTTY.stdout.String())
	}
	go noTTY.stdinW.Write([]byte("\n"))
	time.Sleep(300 * time.Millisecond)
	if n := noTTY.stdinR.calls.Load(); n != 0 {
		t.Errorf("stdin that is no terminal was read %d time(s)", n)
	}
}

// covers: MA-373, R23
// `serve` records itself for `serve -link` — a 0600 instance file in the
// per-account directory, keyed by the archive, with its address and a key — and
// removes it at exit; `serve -link -out DIR` then returns a fresh 10-minute link
// through the HMAC handoff, each call a different one, and a link that redeems.
// A serve with no terminal and no display, as under nohup or systemd, is reached
// this way [CC18].
func TestServeLinkReturnsAFreshLinkThroughTheInstanceFile(t *testing.T) {
	out := buildSearchFixture(t)
	g := startSurface(t, rigOpts{stdoutTTY: false, display: false}, serveMain, out, serveArgs(out)...)
	path := filepath.Join(g.state, instance.ServeFileName(out))
	info, err := instance.Read(path)
	if err != nil {
		t.Fatalf("serve left no usable instance file: %v", err)
	}
	if want := strings.TrimPrefix(g.origin, "http://"); info.Dashboard != want || info.PID != os.Getpid() || len(info.Key) != 64 {
		t.Errorf("instance file %+v, want address %s, this pid, a 64-hex key", info, want)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("instance file mode %o, want 600", fi.Mode().Perm())
		}
	}

	ask := func() (string, error) {
		env := *g.env
		var o syncBuf
		env.Stdout = &o
		env.Ready = nil
		err := serveMain(context.Background(), []string{"-link", "-out", out}, &env)
		return strings.TrimSpace(o.String()), err
	}
	l1, err := ask()
	if err != nil || !strings.HasPrefix(l1, g.origin+"/_open?t=") {
		t.Fatalf("serve -link: %q, %v; want a link on %s", l1, err, g.origin)
	}
	l2, err := ask() // asked again at once: the instance says busy, the quiet caller waits and asks again
	if err != nil || l2 == l1 {
		t.Fatalf("a second serve -link: %q, %v; want a different link", l2, err)
	}
	for i, l := range []string{l1, l2} {
		if code, _, _ := fetch(t, l); code != http.StatusOK {
			t.Errorf("serve -link's link %d: status %d, want 200", i, code)
		}
	}
	if len(g.openedLinks()) != 0 {
		t.Errorf("serve -link made the serve open a browser")
	}

	// A wrong key gets nothing.
	was := linkWindow
	linkWindow = time.Second
	defer func() { linkWindow = was }()
	k, _ := instance.NewKey()
	bad := info
	bad.Key = k
	if err := instance.Write(path, bad); err != nil {
		t.Fatal(err)
	}
	_, err = ask()
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errText(err), assure.Code(1), assure.Names("did not prove", "mailarchive serve -out"), assure.Forbid("_open?t="))
	// Put the right key back so the exit cleanup recognises its file, then stop.
	if err := instance.Write(path, info); err != nil {
		t.Fatal(err)
	}
	g.stop()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("the instance file survived the serve's exit: %v", err)
	}
	_, err = ask()
	rc = 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errText(err), assure.Code(1), assure.Names("no running serve found", "mailarchive serve -out"))
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// covers: MA-373, R23
// A LAN serve's session cookie carries a Max-Age of one working day, so closing the
// browser does not end its session; a loopback serve's never does [CC18].
func TestLANServeCookieOutlivesTheBrowser(t *testing.T) {
	env := newLinkRig(t, rigOpts{}).env
	for _, c := range []struct {
		addr string
		want string
	}{{"0.0.0.0:8099", "Max-Age=86400"}, {"127.0.0.1:8099", ""}} {
		s := newServeSession(c.addr, env)
		h := localweb.Wrap(localweb.Surface{Name: "serve", Session: s}, http.NotFoundHandler())
		req, _ := http.NewRequest(http.MethodGet, localweb.OpenPath(s.Mint(localweb.LinkTTL)), nil)
		req.Host = "127.0.0.1:8099"
		rec := &recorder{header: http.Header{}}
		h.ServeHTTP(rec, req)
		cookies := strings.Join(rec.header.Values("Set-Cookie"), "\n")
		if c.want == "" && strings.Contains(cookies, "Max-Age") || c.want != "" && !strings.Contains(cookies, c.want) {
			t.Errorf("%s: cookies %q, want Max-Age %q", c.addr, cookies, c.want)
		}
	}
}

// recorder is the smallest ResponseWriter that keeps the headers.
type recorder struct {
	header http.Header
	code   int
}

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) Write(b []byte) (int, error) { return len(b), nil }
func (r *recorder) WriteHeader(code int)        { r.code = code }

// covers: MA-372, MA-373, R23, X9
// `serve`, built as the binary builds it and reached by the link it opened, loads
// whole under /s/<P>/: the page, its script, search and facets, go-back and the
// message links, none outside the prefix, none locked or missing; and `setup`, on
// a free port of its own (not the desktop's 8097) and reached the same way, loads
// its page, its script and the endpoint the script posts to, whose CSRF token is
// delivered only inside the session [CC17, CC18].
func TestProductionSurfacesLoadUnderThePrefix(t *testing.T) {
	out := buildSearchFixture(t)
	g := startSurface(t, rigOpts{stdoutTTY: true, display: true}, serveMain, out, serveArgs(out)...)
	loaded, prefix := localwebtest.BrowseFrom(t, g.openedLinks()[0], "")
	assure.Reached(t, loaded, "URLs serve loaded")
	if bad := localwebtest.Check(loaded, prefix); len(bad) != 0 {
		t.Errorf("serve: %s", strings.Join(bad, "; "))
	}
	for _, want := range []string{"app.js", "api/search", "api/facets", "goback"} {
		found := false
		for _, l := range loaded {
			if strings.HasSuffix(l.URL, want) && l.Status == http.StatusOK {
				found = true
			}
		}
		if !found {
			t.Errorf("serve: %s was not loaded under the prefix: %v", want, loaded)
		}
	}
	// Outside the prefix the same server answers only the locked page.
	if code, body, _ := fetch(t, g.origin+"/api/search?q=invoice"); code != http.StatusUnauthorized || strings.Contains(body, "Acme") {
		t.Errorf("serve outside the prefix: status %d (body %q), want the locked 401", code, body)
	}

	cfg := filepath.Join(t.TempDir(), "graph-config.json")
	s := startSurface(t, rigOpts{stdoutTTY: true, display: true}, setupMain, "", "-config", cfg)
	if strings.HasSuffix(s.origin, ":8097") || strings.HasSuffix(s.origin, ":0") {
		t.Errorf("setup's default address is %s: it must take a free port, not 8097", s.origin)
	}
	if !strings.Contains(s.stdout.String(), "Microsoft 365 setup:  "+s.origin+"/") {
		t.Errorf("setup's stdout lacks its address:\n%s", s.stdout.String())
	}
	loaded, prefix = localwebtest.BrowseFrom(t, s.openedLinks()[0], "")
	if bad := localwebtest.Check(loaded, prefix); len(bad) != 0 {
		t.Errorf("setup: %s", strings.Join(bad, "; "))
	}
	reached := false
	for _, l := range loaded {
		if strings.HasSuffix(l.URL, "api/setup") && l.Status == http.StatusMethodNotAllowed {
			reached = true
		}
	}
	if !reached || !loadedOK(loaded, "setup.js") {
		t.Errorf("setup: its script or its endpoint was not reached under the prefix: %v", loaded)
	}
	if code, body, _ := fetch(t, s.origin+"/"); code != http.StatusUnauthorized || strings.Contains(body, `name="csrf"`) {
		t.Errorf("setup outside the prefix: status %d, want the locked page without a CSRF token", code)
	}
	if len(s.printed()) != 1 {
		t.Errorf("setup printed %d links, want one", len(s.printed()))
	}
}

func loadedOK(loaded []localwebtest.Loaded, suffix string) bool {
	for _, l := range loaded {
		if strings.HasSuffix(l.URL, suffix) && l.Status == http.StatusOK {
			return true
		}
	}
	return false
}

// covers: MA-373, R12
// `setup` still refuses a non-loopback bind, naming the address: the wizard writes
// credentials.
func TestSetupRefusesANonLoopbackBind(t *testing.T) {
	g := newLinkRig(t, rigOpts{})
	err := setupMain(context.Background(), []string{"-addr", "0.0.0.0:0", "-config", filepath.Join(t.TempDir(), "c.json")}, g.env)
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errText(err), assure.Code(1), assure.Names("0.0.0.0:0", "not a loopback address"))
}
