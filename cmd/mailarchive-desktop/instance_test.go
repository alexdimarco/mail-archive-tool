package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/instance"
)

// recorder is what a launch's surroundings were asked to do: the browser links it
// was handed, the refusals and the links it was asked to show.
type recorder struct {
	mu       sync.Mutex
	opened   []string
	refusals []string
	links    []string
	openErr  error
	arrive   bool // the "browser" redeems the link it is handed, as a real one would
}

func (r *recorder) open(u string) error {
	r.mu.Lock()
	r.opened = append(r.opened, u)
	err, arrive := r.openErr, r.arrive
	r.mu.Unlock()
	if err != nil {
		return err
	}
	if arrive {
		go http.Get(u)
	}
	return nil
}

func (r *recorder) refuse(s string) { r.mu.Lock(); r.refusals = append(r.refusals, s); r.mu.Unlock() }
func (r *recorder) show(s string)   { r.mu.Lock(); r.links = append(r.links, s); r.mu.Unlock() }

func (r *recorder) snapshot() (opened, refusals, links []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.opened...), append([]string(nil), r.refusals...), append([]string(nil), r.links...)
}

// dRig is a desktop launch in-process: a per-account state directory that is a
// temp dir, no browser, no dialog, no real account — every seam is a recorder.
type dRig struct {
	t       *testing.T
	dir     string // the per-account state directory
	cfgPath string
	out     string // an archive with an index, so the reader has something to serve
	rec     *recorder
	info    instance.Info
	cancel  context.CancelFunc
	done    chan error

	probe      string
	probeCalls int
	endOld     bool
	endCalls   int
	session    string
	prefix     string // the session's path prefix, learned by cookieFromLink

	exportDir string // the Extraction workspace's state directory ("" = none: it writes nothing)
	policyOff bool   // the machine policy that turns the workspace off
}

func newDRig(t *testing.T) *dRig {
	t.Helper()
	g := &dRig{t: t, dir: filepath.Join(t.TempDir(), "state"), rec: &recorder{arrive: true}}
	root := t.TempDir()
	g.cfgPath = filepath.Join(root, "graph-config.json")
	g.out = filepath.Join(root, "archive")
	if err := os.MkdirAll(g.out, 0o755); err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(filepath.Join(g.out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	ix.Close()
	return g
}

// env builds a launch environment over rec. WatchEvery is short and Window small
// so a launch that has to wait does not make the test slow.
func (g *dRig) env(rec *recorder) *launchEnv {
	return &launchEnv{
		Stdout: io.Discard, Stderr: io.Discard,
		StateDir: func() (string, error) { return g.dir, nil },
		Session:  g.session,
		Open:     rec.open, ShowRefusal: rec.refuse, ShowLink: rec.show,
		Probe:      func() string { g.probeCalls++; return g.probe },
		EndOld:     func() bool { g.endCalls++; return g.endOld },
		WatchEvery: 50 * time.Millisecond, Window: 2 * time.Second,
		ExportDir: func() string { return g.exportDir }, WorkspaceOff: func() bool { return g.policyOff },
	}
}

func (g *dRig) args(extra ...string) []string {
	return append([]string{"-config", g.cfgPath, "-out", g.out}, extra...)
}

// start launches the account's instance and returns once it is serving.
func (g *dRig) start(extra ...string) {
	g.t.Helper()
	env := g.env(g.rec)
	ready := make(chan instance.Info, 1)
	env.Ready = func(i instance.Info) { ready <- i }
	ctx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	g.done = make(chan error, 1)
	go func() { g.done <- launch(ctx, g.args(extra...), env) }()
	g.t.Cleanup(g.stop)
	select {
	case g.info = <-ready:
	case err := <-g.done:
		g.t.Fatalf("the launch ended before serving: %v (refusals %v)", err, g.rec.refusals)
	case <-time.After(15 * time.Second):
		g.t.Fatalf("the instance was not serving after 15 s")
	}
}

func (g *dRig) stop() {
	if g.cancel == nil {
		return
	}
	g.cancel()
	select {
	case <-g.done:
	case <-time.After(10 * time.Second):
		g.t.Errorf("the instance did not stop")
	}
	g.cancel = nil
}

// second is another launch of the same account, with surroundings of its own; it
// returns what it did.
func (g *dRig) second(extra ...string) (*recorder, error) {
	g.t.Helper()
	rec := &recorder{arrive: true}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return rec, launch(ctx, g.args(extra...), g.env(rec))
}

func isRefused(err error) bool {
	var r *refusedError
	return errors.As(err, &r)
}

var openRe = regexp.MustCompile(`^http://127\.0\.0\.1:\d+/_open\?t=[A-Za-z0-9_-]+$`)

func get(t *testing.T, u string, cookies ...*http.Cookie) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, string(b)
}

// covers: MA-376, MA-277, R23, R12, S43
// The first launch is the account's instance: it takes the per-account lock,
// removes a stale instance file, binds the dashboard and the reader on ports of
// their own (never 8097 or 8099), writes a 0600 instance file with both addresses,
// the config path and a 32-byte key, and opens the browser once at its own
// one-time link; the dashboard and the reader each show only the locked page
// until that link is redeemed, and the cookie it gives opens the reader's port
// too [CC21, ruling OQ-9].
func TestDesktopFirstLaunchIsTheInstance(t *testing.T) {
	g := newDRig(t)
	if err := os.MkdirAll(g.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(instance.FilePathIn(g.dir, ""), []byte(`{"v":1,"pid":1,"dashboard":"127.0.0.1:1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	g.start()

	info, err := instance.Read(instance.FilePathIn(g.dir, ""))
	if err != nil {
		t.Fatalf("no usable instance file: %v", err)
	}
	if info.PID != os.Getpid() || info.Config != g.cfgPath || info.Dashboard == "127.0.0.1:1" || len(info.Key) != 64 {
		t.Errorf("instance file %+v: the stale one was not replaced by this instance's", info)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(instance.FilePathIn(g.dir, "")); fi.Mode().Perm() != 0o600 {
			t.Errorf("instance file mode %o, want 600", fi.Mode().Perm())
		}
	}
	for name, a := range map[string]string{"dashboard": info.Dashboard, "reader": info.Reader} {
		_, p, _ := net.SplitHostPort(a)
		if p == "0" || p == "8097" || p == "8099" {
			t.Errorf("the %s is on port %s, want an ephemeral port of its own", name, p)
		}
	}
	if info.Dashboard == info.Reader {
		t.Errorf("dashboard and reader share %s", info.Dashboard)
	}
	if !instance.LockIsHeld(g.dir, "") {
		t.Errorf("the instance does not hold the per-account lock")
	}

	opened, _, _ := g.rec.snapshot()
	if len(opened) != 1 || !openRe.MatchString(opened[0]) || !strings.HasPrefix(opened[0], "http://"+info.Dashboard+"/_open?t=") {
		t.Fatalf("the browser was handed %q, want one link on the dashboard %s", opened, info.Dashboard)
	}
	// Only the locked page until the link is redeemed: dashboard and reader alike.
	for _, a := range []string{info.Dashboard, info.Reader} {
		if code, body := get(t, "http://"+a+"/"); code != http.StatusUnauthorized || !strings.Contains(body, "has to be opened from the app") {
			t.Errorf("%s with no session: %d, want the desktop's locked page", a, code)
		}
	}
	// A fresh link, as -copy-link makes one (the opener's was redeemed by the
	// recorded "browser"): its cookie opens the dashboard, whose header counts the
	// sessions opened this run, and the reader's port as well.
	_, dash := g.cookieFromLink()
	code, page := get(t, "http://"+info.Dashboard+g.prefix, dash)
	if m := regexp.MustCompile(`browser sessions opened this run: (\d+)`).FindStringSubmatch(page); code != http.StatusOK || m == nil || m[1] == "0" {
		t.Errorf("the dashboard with the session: %d; its header should count the sessions opened:\n%.600s", code, page)
	}
	if code, _ := get(t, "http://"+info.Reader+g.prefix, dash); code == http.StatusUnauthorized {
		t.Errorf("the cookie of the dashboard's link was refused by the reader's port")
	}
}

// covers: MA-376, MA-281, R23, R12, S41, S43
// A second launch of the same account hands off to the instance after it has
// proved itself with its key: the instance's own browser opener is called once and
// the launch returns nil; the launch's own opener is never called and no second
// instance starts. A shortcut click while the app runs reaches this account's
// dashboard, and only this account's [CC21; ruling J7: the reconnect of MA-281
// becomes the verified handoff].
func TestDesktopReconnectsWhenAlreadyRunning(t *testing.T) {
	g := newDRig(t)
	g.start()
	before, _, _ := g.rec.snapshot()
	rec2, err := g.second()
	if err != nil {
		t.Fatalf("a launch while the app runs should reconnect, not fail: %v", err)
	}
	after, _, _ := g.rec.snapshot()
	if len(after) != len(before)+1 || !openRe.MatchString(after[len(after)-1]) || !strings.HasPrefix(after[len(after)-1], "http://"+g.info.Dashboard+"/") {
		t.Errorf("the instance's opener was handed %q after the handoff, want one more link on %s", after[len(before):], g.info.Dashboard)
	}
	opened2, refusals2, links2 := rec2.snapshot()
	if len(opened2) != 0 || len(refusals2) != 0 || len(links2) != 0 {
		t.Errorf("the second launch opened %q, refused %q, showed %q; it should do nothing itself", opened2, refusals2, links2)
	}
	info, err := instance.Read(instance.FilePathIn(g.dir, ""))
	if err != nil || info.PID != g.info.PID || info.Dashboard != g.info.Dashboard {
		t.Errorf("a second instance replaced the first: %+v, %v", info, err)
	}
}

// covers: MA-376, MA-281, R23, S41
// An opener that errors in the instance → "opened: false" and the launch shows the
// fresh link through the dialog seam, a link that works; a second handoff inside a
// second is "busy" and counts as success, opening nothing more; and -no-browser,
// the background Startup launch, suppresses the open — on the first launch and on
// a reconnect [CC20, CC21].
func TestDesktopNoBrowserSuppressesOpen(t *testing.T) {
	g := newDRig(t)
	g.rec.arrive = true
	g.start("-no-browser")
	if opened, _, _ := g.rec.snapshot(); len(opened) != 0 {
		t.Fatalf("-no-browser opened %q on the first launch", opened)
	}
	rec2, err := g.second("-no-browser")
	if err != nil {
		t.Fatalf("a -no-browser reconnect: %v", err)
	}
	if o1, _, _ := g.rec.snapshot(); len(o1) != 0 {
		t.Errorf("a -no-browser reconnect made the instance open %q", o1)
	}
	if o2, r2, l2 := rec2.snapshot(); len(o2)+len(r2)+len(l2) != 0 {
		t.Errorf("a -no-browser reconnect opened %q, refused %q, showed %q", o2, r2, l2)
	}

	// An opener that errors: the launch shows a fresh link.
	time.Sleep(1100 * time.Millisecond)
	g.rec.mu.Lock()
	g.rec.openErr = errors.New("no default browser")
	g.rec.mu.Unlock()
	rec3, err := g.second()
	if err != nil {
		t.Fatal(err)
	}
	_, _, links := rec3.snapshot()
	if len(links) != 1 || !openRe.MatchString(links[0]) {
		t.Fatalf("an instance whose opener errors: the launch showed %q, want one fresh link", links)
	}
	if code, _ := get(t, links[0]); code != http.StatusOK {
		t.Errorf("the link in the dialog: status %d, want 200", code)
	}

	// Busy: a second handoff within the second opens nothing and exits cleanly.
	g.rec.mu.Lock()
	g.rec.openErr = nil
	g.rec.mu.Unlock()
	time.Sleep(1100 * time.Millisecond)
	o0, _, _ := g.rec.snapshot()
	if _, err := g.second(); err != nil {
		t.Fatal(err)
	}
	rec5, err := g.second()
	if err != nil {
		t.Fatalf("a handoff within the second: %v", err)
	}
	o1, _, _ := g.rec.snapshot()
	if len(o1) != len(o0)+1 {
		t.Errorf("two handoffs inside a second opened %d browsers, want one", len(o1)-len(o0))
	}
	if _, r, l := rec5.snapshot(); len(r)+len(l) != 0 {
		t.Errorf("the busy launch refused %q or showed %q", r, l)
	}
}

// covers: MA-376, R23
// -copy-link, the Start-menu shortcut for a default browser that cannot be opened:
// with the app running it asks for a link without a browser and shows it through
// the dialog seam (the instance's opener is not used); with no app running it
// starts the instance without opening a browser and shows its own link [CC20].
func TestDesktopCopyLinkShowsALinkOnly(t *testing.T) {
	g := newDRig(t)
	g.start()
	time.Sleep(1100 * time.Millisecond)
	before, _, _ := g.rec.snapshot()
	rec2, err := g.second("-copy-link")
	if err != nil {
		t.Fatal(err)
	}
	after, _, _ := g.rec.snapshot()
	_, _, links := rec2.snapshot()
	if len(after) != len(before) || len(links) != 1 || !openRe.MatchString(links[0]) || !strings.HasPrefix(links[0], "http://"+g.info.Dashboard+"/") {
		t.Errorf("-copy-link with the app running: instance opens %d -> %d, dialog %q; want no opening and one link on %s", len(before), len(after), links, g.info.Dashboard)
	}
	if code, _ := get(t, links[0]); code != http.StatusOK {
		t.Errorf("the copied link: status %d, want 200", code)
	}
	g.stop()

	h := newDRig(t)
	h.rec.arrive = false
	h.cfgPath, h.out = g.cfgPath, g.out
	h.start("-copy-link")
	if o, _, _ := h.rec.snapshot(); len(o) != 0 {
		t.Errorf("-copy-link with no app running opened a browser: %q", o)
	}
	waitUntil(t, "the dialog link", func() bool { _, _, l := h.rec.snapshot(); return len(l) == 1 })
	_, _, l := h.rec.snapshot()
	if !strings.HasPrefix(l[0], "http://"+h.info.Dashboard+"/_open?t=") {
		t.Errorf("the link shown is %q, want one on the started instance %s", l[0], h.info.Dashboard)
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// covers: MA-376, R23
// A deleted instance file with the lock held is rewritten by the holder, and a
// launch that arrived while it was missing finds it on its retry and hands off
// [CC21].
func TestDesktopRetriesWhileTheInstanceFileIsRewritten(t *testing.T) {
	g := newDRig(t)
	g.start()
	time.Sleep(1100 * time.Millisecond)
	if err := os.Remove(instance.FilePathIn(g.dir, "")); err != nil {
		t.Fatal(err)
	}
	before, _, _ := g.rec.snapshot()
	if _, err := g.second(); err != nil {
		t.Fatalf("the launch did not reach the instance whose file was rewritten: %v", err)
	}
	after, _, _ := g.rec.snapshot()
	if len(after) != len(before)+1 {
		t.Errorf("the instance opened %d browsers after the retry, want one", len(after)-len(before))
	}
	if _, err := instance.Read(instance.FilePathIn(g.dir, "")); err != nil {
		t.Errorf("the holder did not rewrite its instance file: %v", err)
	}
}

// covers: MA-376, R23, R12
// A launch with a different -config or a different archive is a visible refusal
// naming what the running instance holds, and no handoff is made: the instance's
// browser is not opened [CC21].
func TestDesktopRefusesAnotherConfiguration(t *testing.T) {
	g := newDRig(t)
	g.start()
	before, _, _ := g.rec.snapshot()
	other := filepath.Join(t.TempDir(), "other.json")
	rec2, err := g.second("-config", other)
	if !isRefused(err) {
		t.Fatalf("a launch with another -config: %v, want a refusal", err)
	}
	if _, r, _ := rec2.snapshot(); len(r) != 1 || r[0] != otherConfigText(g.cfgPath) {
		t.Errorf("refusal %q, want %q", r, otherConfigText(g.cfgPath))
	}
	rec3, err := g.second("-out", t.TempDir())
	if !isRefused(err) {
		t.Fatalf("a launch with another archive: %v, want a refusal", err)
	}
	if _, r, _ := rec3.snapshot(); len(r) != 1 || !strings.Contains(r[0], "another archive or configuration") {
		t.Errorf("refusal %q", r)
	}
	if after, _, _ := g.rec.snapshot(); len(after) != len(before) {
		t.Errorf("a refused launch made the instance open a browser")
	}
}

// covers: MA-376, R23
// Another logon session of the same account — a second sign-in on an RDS host, or
// fast user switching — gets an instance of its own (a lock and an instance file
// keyed by its session), whose browser opens in its own session, instead of a
// handoff that would open one the user of that session cannot see [CC21].
func TestDesktopAnotherLogonSessionGetsItsOwnInstance(t *testing.T) {
	a := newDRig(t)
	a.session = "1"
	a.start()
	b := newDRig(t)
	b.dir, b.cfgPath, b.out, b.session = a.dir, a.cfgPath, a.out, "2"
	b.start()
	if a.info.Dashboard == b.info.Dashboard || a.info.PID == 0 {
		t.Errorf("two sessions share an instance: %s and %s", a.info.Dashboard, b.info.Dashboard)
	}
	ia, ea := instance.Read(instance.FilePathIn(a.dir, "1"))
	ib, eb := instance.Read(instance.FilePathIn(a.dir, "2"))
	if ea != nil || eb != nil || ia.Session != "1" || ib.Session != "2" {
		t.Errorf("per-session instance files: %+v %v / %+v %v", ia, ea, ib, eb)
	}
	if o, _, _ := b.rec.snapshot(); len(o) != 1 || !strings.HasPrefix(o[0], "http://"+b.info.Dashboard+"/") {
		t.Errorf("the second session's browser was handed %q, want a link on its own instance %s", o, b.info.Dashboard)
	}
}

// squatter is a listener that is not MailArchive: it answers a handoff with a
// well-formed reply under the wrong proof, and counts what reached it.
type squatter struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits int
}

func newSquatter(t *testing.T, silent bool) *squatter {
	s := &squatter{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits++
		s.mu.Unlock()
		if silent {
			time.Sleep(2 * time.Second)
			return
		}
		io.WriteString(w, `{"opened":true,"p":"`+strings.Repeat("0", 64)+`"}`)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *squatter) addr() string { return strings.TrimPrefix(s.srv.URL, "http://") }
func (s *squatter) count() int   { s.mu.Lock(); defer s.mu.Unlock(); return s.hits }

// covers: MA-376, R23, F4
// A squatter that answers at the recorded port with a wrong proof is never
// opened: with the lock held by a live process the launch retries for its window
// and then refuses visibly, naming the lock's pid (the instance is wedged), and
// opens nothing; with the lock free — the instance is gone and its file stale —
// the launch starts its own instance on a port of its own and its browser is
// handed only its own address, never the squatter's [CC20, CC21, P8].
func TestDesktopNeverOpensASquatter(t *testing.T) {
	for _, silent := range []bool{false, true} {
		g := newDRig(t)
		sq := newSquatter(t, silent)
		old := instance.CallTimeout
		instance.CallTimeout = 300 * time.Millisecond
		t.Cleanup(func() { instance.CallTimeout = old })
		held, err := instance.Acquire(g.dir, "")
		if err != nil {
			t.Fatal(err)
		}
		k, _ := instance.NewKey()
		if err := instance.Write(held.FilePath(), instance.Info{PID: os.Getpid(), Config: g.cfgPath, Dashboard: sq.addr(), Key: k}); err != nil {
			t.Fatal(err)
		}
		env := g.env(g.rec)
		env.Window = 1200 * time.Millisecond
		err = launch(context.Background(), g.args(), env)
		if !isRefused(err) {
			t.Fatalf("silent=%v: a launch against a wedged instance: %v, want a visible refusal", silent, err)
		}
		pid := instance.HolderPID(g.dir, "")
		if _, r, _ := g.rec.snapshot(); len(r) != 1 || r[0] != wedgedText(pid) || (runtime.GOOS != "windows" && !strings.Contains(r[0], strconv.Itoa(os.Getpid()))) {
			t.Errorf("silent=%v: refusal %q, want %q naming the lock's pid", silent, r, wedgedText(pid))
		}
		if o, _, l := g.rec.snapshot(); len(o)+len(l) != 0 {
			t.Errorf("silent=%v: a squatter's launch opened %q / showed %q", silent, o, l)
		}
		if sq.count() == 0 {
			t.Errorf("silent=%v: the squatter was never contacted — the test did not exercise it", silent)
		}
		held.Close()
	}

	// The lock is free: the stale file naming a squatter is replaced, never believed.
	g := newDRig(t)
	sq := newSquatter(t, false)
	if err := os.MkdirAll(g.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	k, _ := instance.NewKey()
	if err := instance.Write(instance.FilePathIn(g.dir, ""), instance.Info{PID: 1, Config: g.cfgPath, Dashboard: sq.addr(), Key: k}); err != nil {
		t.Fatal(err)
	}
	g.start()
	opened, _, _ := g.rec.snapshot()
	if len(opened) != 1 || strings.Contains(opened[0], sq.addr()) || !strings.HasPrefix(opened[0], "http://"+g.info.Dashboard+"/") {
		t.Errorf("with the lock free the browser was handed %q, want only the new instance %s", opened, g.info.Dashboard)
	}
	if sq.count() != 0 || g.info.Dashboard == sq.addr() {
		t.Errorf("the squatter at %s was contacted %d times or reused", sq.addr(), sq.count())
	}
}

// covers: MA-376, MA-384, R23, X6
// With the dashboard's own address left to the program, it is always an ephemeral
// port and the reader another; what answers at MailArchive's former address
// 127.0.0.1:8097 is named, never silent: another program → the "former address"
// notice in the dashboard's header and in `status`; an older instance with no
// /_handoff → the pre-upgrade notice, unless it is this account's and was ended,
// in which case nothing is said. An explicit -addr is not probed [CC22, CC23,
// ruling OQ-9].
func TestDesktopNamesWhatHoldsTheFormerAddress(t *testing.T) {
	for _, c := range []struct {
		name    string
		probe   string
		endOld  bool
		former  string
		endCall int
	}{
		{"another program", instance.FormerProgram, false, instance.FormerProgram, 0},
		{"an older instance that cannot be ended", instance.FormerPreUpgrade, false, instance.FormerPreUpgrade, 1},
		{"an older instance of this account, ended", instance.FormerPreUpgrade, true, "", 1},
		{"nothing there", "", false, "", 0},
	} {
		g := newDRig(t)
		g.probe, g.endOld = c.probe, c.endOld
		g.rec.arrive = false
		g.start()
		if g.probeCalls != 1 || g.endCalls != c.endCall {
			t.Errorf("%s: probed %d times and tried to end %d, want 1 and %d", c.name, g.probeCalls, g.endCalls, c.endCall)
		}
		for _, a := range []string{g.info.Dashboard, g.info.Reader} {
			if strings.HasSuffix(a, ":8097") || strings.HasSuffix(a, ":8099") {
				t.Errorf("%s: %s is a fixed port", c.name, a)
			}
		}
		if g.info.Former != c.former {
			t.Errorf("%s: recorded former %q, want %q", c.name, g.info.Former, c.former)
		}
		// The dashboard's header: through the session the opener's link gives.
		_, ck := g.cookieFromLink()
		_, page := get(t, "http://"+g.info.Dashboard+g.prefix, ck)
		want := instance.NoticeFor(c.former)
		switch {
		case want == "" && strings.Contains(page, `id="formerNotice"`):
			t.Errorf("%s: the header names a former address though none holds it", c.name)
		case want != "" && !strings.Contains(strings.ReplaceAll(page, "&#39;", "'"), want):
			t.Errorf("%s: the header lacks %q", c.name, want)
		}
		// `status`: the same finding, from the instance file, as a WARN.
		in := health.Input{Out: g.out, DesktopFormer: instance.FormerIn(g.dir, "")}
		rep := health.Assess(in, time.Now())
		code := map[string]string{instance.FormerProgram: "desktop_former_address", instance.FormerPreUpgrade: "desktop_preupgrade"}[c.former]
		if c.former != "" && !strings.Contains(strings.Join(rep.Codes, ","), code) {
			t.Errorf("%s: status codes %q lack %s", c.name, rep.Codes, code)
		}
		if c.former == "" && in.DesktopFormer != "" {
			t.Errorf("%s: status names a former address though none holds it", c.name)
		}
		g.stop()
	}

	// An explicit -addr is the user's own choice: the former address is not probed.
	g := newDRig(t)
	g.probe = instance.FormerProgram
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	g.start("-addr", addr)
	if g.probeCalls != 0 || g.info.Dashboard != addr || g.info.Former != "" {
		t.Errorf("with -addr %s: probed %d times, dashboard %s, former %q", addr, g.probeCalls, g.info.Dashboard, g.info.Former)
	}
}

// cookieFromLink makes a fresh link through the instance's own handoff, as
// `-copy-link` does (the opener's link was spent by the recorded browser), opens it
// as a browser would and returns the session cookie it gave; g.prefix is set to the
// session's path prefix.
func (g *dRig) cookieFromLink() (string, *http.Cookie) {
	g.t.Helper()
	key, _ := g.info.KeyBytes()
	time.Sleep(1100 * time.Millisecond)
	rep, err := instance.Call(context.Background(), g.info.Dashboard, key, true)
	if err != nil || rep.Link == "" {
		g.t.Fatalf("a quiet handoff for a link: %+v, %v", rep, err)
	}
	resp, err := http.Get(rep.Link)
	if err != nil {
		g.t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if m := regexp.MustCompile(`url=(/s/[a-z0-9]+/)`).FindStringSubmatch(string(b)); m != nil {
		g.prefix = m[1]
	}
	for _, c := range resp.Cookies() {
		if c.Name == "mailarchive_session" {
			return rep.Link, c
		}
	}
	g.t.Fatalf("the link gave no session cookie")
	return "", nil
}

// covers: MA-376, R12
// An explicit -addr that is in use is a visible refusal in the §7.6 words, exit 1
// (a refusal, shown once), and the lock it took is released; an explicit
// -reader-addr in use is an error naming it and leaves nothing bound or locked; a
// launch with no per-account folder to be had refuses visibly and binds nothing
// [CC20, CC21].
func TestDesktopRefusesVisibly(t *testing.T) {
	g := newDRig(t)
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	addr := busy.Addr().String()
	rec := &recorder{}
	err = launch(context.Background(), g.args("-addr", addr), g.env(rec))
	if !isRefused(err) {
		t.Fatalf("an explicit -addr in use: %v, want a refusal", err)
	}
	want := "mailarchive-desktop: -addr " + addr + " is in use by another program. Why: the dashboard needs its own port. Do this: leave -addr unset (any free port is used) or choose a free port."
	if _, r, _ := rec.snapshot(); len(r) != 1 || r[0] != want {
		t.Errorf("refusal %q, want %q", r, want)
	}
	if instance.LockIsHeld(g.dir, "") {
		t.Errorf("a refused launch kept the per-account lock")
	}

	err = launch(context.Background(), g.args("-reader-addr", addr), g.env(rec))
	if err == nil || isRefused(err) || !strings.Contains(err.Error(), "archive reader could not bind "+addr) {
		t.Errorf("an explicit -reader-addr in use: %v, want an error naming it", err)
	}
	if instance.LockIsHeld(g.dir, "") {
		t.Errorf("the launch kept the lock after the reader could not bind")
	}

	noDir := g.env(rec)
	noDir.StateDir = func() (string, error) { return "", errors.New("$HOME is not defined") }
	rec2 := &recorder{}
	noDir.ShowRefusal = rec2.refuse
	err = launch(context.Background(), g.args(), noDir)
	if !isRefused(err) {
		t.Fatalf("no per-account folder: %v, want a refusal", err)
	}
	_, r, _ := rec2.snapshot()
	assure.Reached(t, r, "refusals for a missing per-account folder")
	if !strings.Contains(r[0], "$HOME is not defined") || !strings.Contains(r[0], "Do this:") {
		t.Errorf("refusal %q does not say why and what to do", r[0])
	}
	if o, _, _ := rec2.snapshot(); len(o) != 0 {
		t.Errorf("a launch with no state directory opened a browser")
	}
}

// covers: MA-376, R23
// The pre-upgrade probe at MailArchive's former address names nothing when nothing
// listens; an older MailArchive Desktop — its dashboard page and no /_handoff
// route — as the pre-upgrade instance; and any other listener, answering or silent,
// as another program, never trusting what it says [CC22, CC23].
func TestProbeFormerAddressClassifiesTheListener(t *testing.T) {
	serve := func(h http.HandlerFunc) string {
		srv := httptest.NewServer(h)
		t.Cleanup(srv.Close)
		return strings.TrimPrefix(srv.URL, "http://")
	}
	dead, _ := net.Listen("tcp", "127.0.0.1:0")
	deadAddr := dead.Addr().String()
	dead.Close()
	oldDashboard := serve(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `<html><head><meta name="csrf" content="abc"><title>MailArchive Desktop</title></head></html>`)
	})
	newInstance := serve(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_handoff" {
			http.Error(w, "refused", http.StatusForbidden)
			return
		}
		io.WriteString(w, `<html><head><meta name="csrf" content="abc"><title>MailArchive Desktop</title></head></html>`)
	})
	lookAlike := serve(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "<title>Sign in</title> enter your code")
	})
	silent := serve(func(w http.ResponseWriter, r *http.Request) { time.Sleep(3 * time.Second) })
	for _, c := range []struct {
		name, addr, want string
	}{
		{"nothing listening", deadAddr, ""},
		{"an older MailArchive Desktop", oldDashboard, instance.FormerPreUpgrade},
		{"an instance that answers /_handoff", newInstance, instance.FormerProgram},
		{"a look-alike page", lookAlike, instance.FormerProgram},
		{"a listener that stays silent", silent, instance.FormerProgram},
	} {
		if got := probeFormer(c.addr, 400*time.Millisecond); got != c.want {
			t.Errorf("%s: probeFormer = %q, want %q", c.name, got, c.want)
		}
	}
}

// covers: MA-376, R23
// parseProcNetTCP reads the listener on a port out of /proc/net/tcp (and tcp6): the
// socket inode of a LISTEN entry on loopback or every interface, nothing for another
// port, a connection that is not listening, or another address [CC22].
func TestParseProcNetTCP(t *testing.T) {
	const tcp = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1FA1 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 424242 1 0000000000000000 100 0 0 10 0
   1: 0100007F:1FA2 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 515151 1 0000000000000000 100 0 0 10 0
   2: 0100007F:1FA1 0100007F:C350 01 00000000:00000000 00:00000000 00000000  1000        0 606060 1 0000000000000000 100 0 0 10 0
   3: 0A00000A:1FA1 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 717171 1 0000000000000000 100 0 0 10 0
`
	if got := parseProcNetTCP(tcp, 8097); got != "424242" {
		t.Errorf("the loopback listener on 8097: %q, want 424242", got)
	}
	if got := parseProcNetTCP(tcp, 8098); got != "515151" {
		t.Errorf("port 8098: %q, want 515151", got)
	}
	if got := parseProcNetTCP(tcp, 9999); got != "" {
		t.Errorf("a port nothing listens on: %q", got)
	}
}
