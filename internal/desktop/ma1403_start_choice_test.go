package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/wizard"
)

// frontSeams replaces the instance's dialog seams for a test (operating-system seams:
// the native dialogs; MB-18) and records what they were asked.
type frontSeams struct {
	mu        sync.Mutex
	views     []choiceView
	infos     []string
	questions []string
	wizards   int
	pick      string        // what the choice answers
	quit      bool          // what the Quit question answers
	hold      chan struct{} // when set, the wizard runs until it is closed
	// choiceHold, when set, keeps the choice on the screen until it is closed.
	choiceHold chan struct{}
	opened     []func() error
	canShow    bool
	// tokens is the activation token in the environment while each choice showed.
	tokens []string
}

func installFrontSeams(t *testing.T) *frontSeams {
	t.Helper()
	s := &frontSeams{pick: pickNone, canShow: true}
	oc, oi, oq, ow, os_ := chooseDialog, infoDialog, quitQuestionDialog, runWizardIn, canShowDialogs
	t.Cleanup(func() {
		chooseDialog, infoDialog, quitQuestionDialog, runWizardIn, canShowDialogs = oc, oi, oq, ow, os_
	})
	chooseDialog = func(_ context.Context, v choiceView) (string, error) {
		s.mu.Lock()
		s.views = append(s.views, v)
		s.tokens = append(s.tokens, os.Getenv(activationEnv))
		p, h := s.pick, s.choiceHold
		s.mu.Unlock()
		if h != nil {
			<-h
		}
		return p, nil
	}
	infoDialog = func(text string) { s.mu.Lock(); s.infos = append(s.infos, text); s.mu.Unlock() }
	quitQuestionDialog = func(q string) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.questions = append(s.questions, q)
		return s.quit
	}
	runWizardIn = func(open func() error) int {
		s.mu.Lock()
		s.wizards++
		s.opened = append(s.opened, open)
		h := s.hold
		s.mu.Unlock()
		if h != nil {
			<-h
		}
		return 0
	}
	canShowDialogs = func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.canShow }
	return s
}

func (s *frontSeams) snap() (views []choiceView, infos, questions []string, wizards int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]choiceView(nil), s.views...), append([]string(nil), s.infos...), append([]string(nil), s.questions...), s.wizards
}

// startInstance launches the account's instance through the `start` verb and returns
// once it serves.
func (g *dRig) startInstance(env *launchEnv, extra ...string) {
	g.t.Helper()
	ready := make(chan instance.Info, 1)
	env.Ready = func(i instance.Info) { ready <- i }
	ctx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	g.done = make(chan error, 1)
	go func() { g.done <- launchStart(ctx, g.args(extra...), env) }()
	g.t.Cleanup(g.stop)
	select {
	case g.info = <-ready:
	case err := <-g.done:
		g.t.Fatalf("the start ended before serving: %v", err)
	case <-time.After(15 * time.Second):
		g.t.Fatalf("the instance was not serving after 15 s")
	}
}

// secondStart is another `start` of the same account.
func (g *dRig) secondStart(env *launchEnv, extra ...string) (*recorder, error) {
	g.t.Helper()
	rec := &recorder{arrive: true}
	if env == nil {
		env = g.env(rec)
	} else {
		env.Open, env.ShowRefusal, env.ShowLink, env.ShowNotice = rec.open, rec.refuse, rec.show, rec.notice
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return rec, launchStart(ctx, g.args(extra...), env)
}

// covers: MA-1403, MA-1400, R50, R39
// One running copy, and the choice (design-menubar §6, §7). Positive twin first: the
// first `start` becomes the instance — it holds the lock, its file advertises the
// actions and records that this version shows no icon, it opens no browser, and it
// shows the choice once, with Desktop the default item when an archive is set up. A
// second `start` while it runs reaches it through the proven hand-off and exits nil
// with nothing of its own opened: the choice shows a second time, in the instance; no
// second instance replaces the first. Picking Open MailArchive Desktop opens the
// instance's own fresh link; a `start -background` while it runs does nothing at all.
func TestMA1403_ASecondStartShowsTheChoiceInTheRunningCopy(t *testing.T) {
	s := installFrontSeams(t)
	// The first choice: the wizard, held open for the whole test (a Cancel, or a wizard
	// that ended with nothing opened, would stop it: MB-16 as F2 extends it).
	s.pick = pickWizard
	wizardOpen := make(chan struct{})
	s.hold = wizardOpen
	t.Cleanup(func() { close(wizardOpen) })
	g := newDRig(t)
	g.startInstance(g.env(g.rec))

	info, err := instance.Read(instance.FilePathIn(g.dir, ""))
	if err != nil || !info.HasAct(instance.ActChoose) || !info.HasAct(instance.ActWizard) || info.Icon != instance.IconNotYet {
		t.Fatalf("the instance file %+v, %v: want the actions advertised and the icon state not-yet", info, err)
	}
	if !instance.LockIsHeld(g.dir, "") {
		t.Fatalf("the started instance does not hold the lock")
	}
	waitUntil(t, "the first choice", func() bool { v, _, _, _ := s.snap(); return len(v) == 1 })
	waitUntil(t, "the wizard picked", func() bool { _, _, _, w := s.snap(); return w == 1 })
	if o, _, _ := g.rec.snapshot(); len(o) != 0 {
		t.Errorf("start opened the browser %q; it shows the choice instead", o)
	}
	if v, _, _, _ := s.snap(); v[0].DefaultItem != choiceDesktopItem || v[0].Quit || v[0].Line != "" {
		t.Errorf("the first choice %+v: want Desktop the default (an archive is set up), no Quit, no lines", v[0])
	}

	// A second start while it runs.
	s.mu.Lock()
	s.pick = pickDesktop
	s.mu.Unlock()
	rec2, err := g.secondStart(nil)
	if err != nil {
		t.Fatalf("a second start: %v", err)
	}
	waitUntil(t, "the second choice", func() bool { v, _, _, _ := s.snap(); return len(v) == 2 })
	if o, r, l := rec2.snapshot(); len(o)+len(r)+len(l)+len(rec2.noticeList()) != 0 {
		t.Errorf("the second start opened %q, refused %q, showed %q, told %q; it should do nothing itself", o, r, l, rec2.noticeList())
	}
	waitUntil(t, "Desktop opened by the instance", func() bool { o, _, _ := g.rec.snapshot(); return len(o) == 1 })
	if o, _, _ := g.rec.snapshot(); !strings.HasPrefix(o[0], "http://"+g.info.Dashboard+"/_open?t=") {
		t.Errorf("Open MailArchive Desktop opened %q, want the instance's own link", o[0])
	}
	if now, err := instance.Read(instance.FilePathIn(g.dir, "")); err != nil || now.PID != g.info.PID || now.Dashboard != g.info.Dashboard {
		t.Errorf("a second instance replaced the first: %+v, %v", now, err)
	}

	// A sign-in launch while it runs does nothing.
	time.Sleep(1100 * time.Millisecond)
	before, _, _, _ := s.snap()
	rec3, err := g.secondStart(nil, "-background")
	if err != nil {
		t.Fatalf("start -background while it runs: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if after, _, _, _ := s.snap(); len(after) != len(before) {
		t.Errorf("start -background showed the choice")
	}
	if o, r, l := rec3.snapshot(); len(o)+len(r)+len(l) != 0 {
		t.Errorf("start -background did %q %q %q", o, r, l)
	}
}

// covers: MA-1430, MA-1405, MA-1404, MA-1407, R50
// One wizard per account, across processes (design-menubar MB-9, D5). Positive twin
// first: the `wizard` verb's hand-off (handOffWizard) reaches the running instance, which
// runs the wizard once and the launch reports it handled; the wizard's Desktop items open
// the instance's own link, never desktop.Launch. While that wizard is open: a second
// hand-off is told "The wizard is already open." in its own session; the choice's Run the
// wizard says it in the instance; Add another archive refuses with the same words; and a
// standalone wizard (the lock file beside desktop.lock) finds it held. Once it ends, the
// lock is free again.
func TestMA1430_OneWizardPerAccount(t *testing.T) {
	s := installFrontSeams(t)
	hold := make(chan struct{})
	s.hold = hold
	g := newDRig(t)
	g.startInstance(g.env(g.rec), "-background")

	rec := &recorder{arrive: true}
	env := g.env(rec)
	if !handOffWizard(context.Background(), env) {
		t.Fatalf("the wizard verb's hand-off was not handled by the running instance")
	}
	waitUntil(t, "the wizard in the instance", func() bool { _, _, _, w := s.snap(); return w == 1 })
	if n := rec.noticeList(); len(n) != 0 {
		t.Errorf("a shown wizard told the launcher %q", n)
	}
	// The wizard's Desktop items open the instance's own link.
	s.mu.Lock()
	open := s.opened[0]
	s.mu.Unlock()
	if err := open(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the instance's link", func() bool { o, _, _ := g.rec.snapshot(); return len(o) == 1 })
	if o, _, _ := g.rec.snapshot(); !strings.HasPrefix(o[0], "http://"+g.info.Dashboard+"/_open?t=") {
		t.Errorf("the wizard opened Desktop at %q, want the instance's own link", o[0])
	}

	// While it is open: a second hand-off is told so, in its own session.
	time.Sleep(1100 * time.Millisecond)
	rec2 := &recorder{arrive: true}
	if !handOffWizard(context.Background(), g.env(rec2)) {
		t.Fatalf("a second wizard hand-off was not handled")
	}
	if n := rec2.noticeList(); len(n) != 1 || n[0] != instance.WizardOpenText {
		t.Errorf("the second wizard launch was told %q, want %q", n, instance.WizardOpenText)
	}
	// A standalone wizard finds the lock held.
	if _, held := wizard.LockWizardIn(g.dir); !held {
		t.Errorf("the wizard lock is not held while the instance's wizard is open")
	}
	// The choice's Run the wizard says so in the instance.
	s.mu.Lock()
	s.pick = pickWizard
	s.mu.Unlock()
	time.Sleep(1100 * time.Millisecond)
	if _, err := g.secondStart(nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the already-open line", func() bool { _, i, _, _ := s.snap(); return len(i) == 1 })
	if _, i, _, w := s.snap(); i[0] != instance.WizardOpenText || w != 1 {
		t.Errorf("the choice's wizard while one is open: info %q, wizards %d; want the line and still one wizard", i, w)
	}
	// Add another archive refuses with the same words.
	d := newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), WizardLock: func() (func(), bool) { return wizard.LockWizardIn(g.dir) }})
	origAdd := addArchiveFn
	t.Cleanup(func() { addArchiveFn = origAdd })
	added := false
	addArchiveFn = func() (string, error) { added = true; return "", nil }
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "http://127.0.0.1:8097/api/add-archive", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	req.Header.Set("X-CSRF-Token", d.csrf)
	d.addArchive(w, req)
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["ok"] != false || got["error"] != instance.WizardOpenText || added {
		t.Errorf("Add another archive while the wizard is open: %v (flow ran: %v), want refused with the wizard's words", got, added)
	}

	// It ends: the lock is free again.
	close(hold)
	waitUntil(t, "the wizard lock let go", func() bool {
		release, held := wizard.LockWizardIn(g.dir)
		if !held {
			release()
		}
		return !held
	})
}

// covers: MA-1402, MA-1427, R50, R12
// The capability rule (design-menubar MB-3, MA-1402 as amended). Positive twin first:
// an instance whose file advertises the actions gets `choose`. An instance whose file has
// no actions (an older version) gets today's open: its browser opens Desktop, and no
// choice is asked of it. An instance that advertises the actions but refuses the second
// form (403) gets the first form once: Desktop opens and the launch ends at once — never
// the 15-second wait and the "not responding" refusal.
func TestMA1402_TheFormIsChosenByCapability(t *testing.T) {
	s := installFrontSeams(t)
	s.pick = pickDesktop // a Desktop page opened in this run keeps the instance running (F2)
	g := newDRig(t)
	g.startInstance(g.env(g.rec), "-background")
	if _, err := g.secondStart(nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the choice", func() bool { v, _, _, _ := s.snap(); return len(v) == 1 })
	waitUntil(t, "Desktop from the choice", func() bool { o, _, _ := g.rec.snapshot(); return len(o) == 1 })

	// An instance file with no actions: rewrite it as an older version would have.
	info, _ := instance.Read(instance.FilePathIn(g.dir, ""))
	info.Acts, info.Icon = nil, ""
	if err := instance.Write(instance.FilePathIn(g.dir, ""), info); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	before, _, _ := g.rec.snapshot()
	if _, err := g.secondStart(nil); err != nil {
		t.Fatalf("start against an instance with no actions: %v", err)
	}
	if after, _, _ := g.rec.snapshot(); len(after) != len(before)+1 {
		t.Errorf("an instance with no actions: its browser opened %d times, want once (today's open)", len(after)-len(before))
	}
	if v, _, _, _ := s.snap(); len(v) != 1 {
		t.Errorf("an instance with no actions was asked for the choice")
	}
	g.stop()

	// An instance that advertises the actions but refuses the second form.
	h := newDRig(t)
	if err := os.MkdirAll(h.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	holder, err := instance.Acquire(h.dir, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(holder.Close)
	keyHex, key := instance.NewKey()
	var mu sync.Mutex
	opens := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ N, P, Act string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Act != "" || !instance.SameMAC(req.P, instance.MAC(key, "handoff-req", req.N)) {
			http.Error(w, "refused", http.StatusForbidden)
			return
		}
		mu.Lock()
		opens++
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"opened": true, "p": instance.MAC(key, "handoff-ok", req.N, "true", "")})
	}))
	t.Cleanup(srv.Close)
	cfgAbs, _ := filepath.Abs(h.cfgPath)
	if err := holder.Publish(instance.Info{PID: os.Getpid(), Dashboard: strings.TrimPrefix(srv.URL, "http://"), Key: keyHex, Config: cfgAbs, Version: "v9.0.0", Acts: instance.KnownActs}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	slow := h.env(&recorder{})
	slow.Window = 10 * time.Second // a retried 403 would take most of this
	rec, err := h.secondStart(slow)
	if err != nil || isRefused(err) {
		t.Fatalf("start against an instance that refuses the second form: %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("the fallback took %v: the 403 was retried", time.Since(start))
	}
	mu.Lock()
	n := opens
	mu.Unlock()
	if _, r, _ := rec.snapshot(); n != 1 || len(r) != 0 {
		t.Errorf("the fallback: %d first-form opens, refusals %q; want one open and no refusal", n, r)
	}
}

// covers: MA-1427, MA-1434, R50
// What the launch tells the person in its own session (design-menubar R50, MB-9, the
// fold's third contradiction). Positive twin first: a launch of the same sign-in gets
// the choice. A launch from another sign-in of this account is told "MailArchive is
// already open in another sign-in of this account…" and nothing shows in the instance. An
// instance that cannot show dialogs answers "can't show here", and the launch opens
// Desktop instead (never a silent end). A choice already on the screen: the launch says
// so.
func TestMA1427_TheLaunchIsToldInItsOwnSession(t *testing.T) {
	s := installFrontSeams(t)
	// The rule is proven on every platform; on Windows the lock is already per sign-in,
	// so there the instance never compares (sessionsSeparate, checked below).
	if (runtime.GOOS == "windows") != sessionsSeparate {
		t.Errorf("sessionsSeparate is %v on %s", sessionsSeparate, runtime.GOOS)
	}
	origSep := sessionsSeparate
	sessionsSeparate = false
	t.Cleanup(func() { sessionsSeparate = origSep })
	g := newDRig(t)
	g.guiSession = "100"
	envI := g.env(g.rec)
	g.startInstance(envI, "-background")

	s.mu.Lock()
	s.pick = pickDesktop // a Desktop page opened in this run keeps the instance running (F2)
	s.mu.Unlock()
	same := g.env(&recorder{})
	if _, err := g.secondStart(same); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the choice for the same sign-in", func() bool { v, _, _, _ := s.snap(); return len(v) == 1 })
	waitUntil(t, "Desktop from it", func() bool { o, _, _ := g.rec.snapshot(); return len(o) == 1 })
	s.mu.Lock()
	s.pick = pickNone
	s.mu.Unlock()

	time.Sleep(1100 * time.Millisecond)
	other := g.env(&recorder{})
	other.GUISession = "200"
	rec, err := g.secondStart(other)
	if err != nil {
		t.Fatal(err)
	}
	if n := rec.noticeList(); len(n) != 1 || n[0] != otherSessionText {
		t.Errorf("a launch from another sign-in was told %q, want %q", n, otherSessionText)
	}
	time.Sleep(200 * time.Millisecond)
	if v, _, _, _ := s.snap(); len(v) != 1 {
		t.Errorf("the choice showed on the other sign-in's screen")
	}

	// Can't show here: the launch opens Desktop instead.
	s.mu.Lock()
	s.canShow = false
	s.mu.Unlock()
	time.Sleep(1100 * time.Millisecond)
	before, _, _ := g.rec.snapshot()
	rec, err = g.secondStart(nil)
	if err != nil {
		t.Fatal(err)
	}
	if after, _, _ := g.rec.snapshot(); len(after) != len(before)+1 {
		t.Errorf("an instance that cannot show dialogs: Desktop opened %d times, want once", len(after)-len(before))
	}
	if n := rec.noticeList(); len(n) != 0 {
		t.Errorf("the can't-show launch was told %q", n)
	}

	// A choice already on the screen: a launch is told so in its own session, and no
	// second choice shows.
	held := make(chan struct{})
	s.mu.Lock()
	s.canShow = true
	s.choiceHold = held
	shownBefore := len(s.views)
	s.mu.Unlock()
	time.Sleep(1100 * time.Millisecond)
	if _, err := g.secondStart(nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the held choice", func() bool { v, _, _, _ := s.snap(); return len(v) == shownBefore+1 })
	time.Sleep(1100 * time.Millisecond)
	rec, err = g.secondStart(nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := rec.noticeList(); len(n) != 1 || n[0] != choiceOpenText {
		t.Errorf("a launch while the choice is on the screen was told %q, want %q", n, choiceOpenText)
	}
	if v, _, _, _ := s.snap(); len(v) != shownBefore+1 {
		t.Errorf("a launch while the choice is on the screen showed a second choice")
	}
	s.mu.Lock()
	s.choiceHold = nil
	s.mu.Unlock()
	close(held)

	f := &front{life: NewLifecycle(context.Background()), dir: g.dir, session: "100"}
	f.mu.Lock()
	f.choosing = true
	f.mu.Unlock()
	if shown, reason := f.act(instance.ActRequest{Act: instance.ActChoose, Session: "100"}); shown || reason != instance.ReasonAlreadyOpen {
		t.Errorf("a choose while the choice is open: %v %q, want already-open", shown, reason)
	}
}

// covers: MA-1434, MA-1431, R50, R33
// The choice's Cancel and its Quit (design-menubar MB-16, MB-14). Positive twin first: a
// Cancel on a later choice leaves the instance running. A Cancel on the choice of the
// launch that started the instance, with nothing running and no Desktop page opened,
// stops it (this version shows no icon to say it runs, X11). When the launcher is newer
// than the instance, the choice says "A newer MailArchive is installed…" and offers Quit;
// Quit stops the instance, and nothing restarts it (no process is started: R33). A
// version that is not a release version is never "newer". `status`'s line names the
// running version and why no icon shows.
func TestMA1434_CancelAndQuitOnTheChoice(t *testing.T) {
	s := installFrontSeams(t)
	g := newDRig(t)
	envI := g.env(g.rec)
	envI.Version = "v1.8.0"
	g.startInstance(envI) // the first choice, Cancel (the seam's default answer)
	select {
	case <-g.done:
	case <-time.After(20 * time.Second):
		t.Fatalf("a Cancel on the first choice with nothing running left the instance running")
	}
	g.cancel = nil
	if instance.LockIsHeld(g.dir, "") {
		t.Errorf("the stopped instance still holds the lock")
	}

	// A later Cancel, once a Desktop page was opened in this run, leaves it running
	// (amended by the S1 fix round's F2: a Cancel with nothing opened ends it, proven in
	// TestMA1434_TheChoiceOrTheWizardEndingIdleEndsTheProgram).
	h := newDRig(t)
	envH := h.env(h.rec)
	envH.Version = "v1.8.0"
	h.startInstance(envH, "-background")
	s.mu.Lock()
	s.pick = pickDesktop
	s.mu.Unlock()
	if _, err := h.secondStart(nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "Desktop opened from the later choice", func() bool { o, _, _ := h.rec.snapshot(); return len(o) == 1 })
	s.mu.Lock()
	s.pick = pickNone
	s.mu.Unlock()
	time.Sleep(1100 * time.Millisecond)
	if _, err := h.secondStart(nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the later choice", func() bool { v, _, _, _ := s.snap(); return len(v) >= 3 })
	time.Sleep(200 * time.Millisecond)
	if !instance.LockIsHeld(h.dir, "") {
		t.Fatalf("a Cancel on a later choice stopped the instance after a Desktop page was opened")
	}
	info, _ := instance.Read(instance.FilePathIn(h.dir, ""))
	if words := instance.RunningWords(info); !strings.Contains(words, "version v1.8.0") || !strings.Contains(words, "shows no icon yet") {
		t.Errorf("status's line %q: want the version and why no icon shows", words)
	}

	// A newer launcher: the line and Quit.
	s.mu.Lock()
	s.pick = pickQuit
	s.mu.Unlock()
	time.Sleep(1100 * time.Millisecond)
	newer := h.env(&recorder{})
	newer.Version = "v1.9.0"
	if _, err := h.secondStart(newer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.done:
	case <-time.After(20 * time.Second):
		t.Fatalf("Quit on the newer-version choice did not stop the instance")
	}
	h.cancel = nil
	v, _, q, _ := s.snap()
	last := v[len(v)-1]
	if !last.Quit || last.Line != newerInstalledText || last.Text() != "A newer MailArchive is installed." {
		t.Errorf("the choice for a newer launcher: %+v, want the newer-installed line and Quit", last)
	}
	if len(q) != 0 {
		t.Errorf("Quit with nothing running asked %q", q)
	}
	for _, c := range []struct {
		a, b string
		want bool
	}{{"v1.9.0", "v1.8.0", true}, {"v1.8.0", "v1.8.0", false}, {"v1.7.9", "v1.8.0", false}, {"1.10.0", "v1.9.9", true}, {"dev", "v1.8.0", false}, {"v1.9.0", "dev", false}, {"v1.9.0-rc1", "v1.8.0", true}} {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// covers: MA-1435, MA-1408, MA-1409, R39
// The entry points' limits (design-menubar MB-17). Positive twin first: a reopen shows
// the choice. Reopens within a second show it once; the launch grants the foreground
// right only to the pid the lock's holder record confirms (here, this process), and never
// when the record names another.
func TestMA1435_ReopenIsRateLimitedAndForegroundGoesOnlyToTheHolder(t *testing.T) {
	s := installFrontSeams(t)
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	f := &front{life: NewLifecycle(context.Background()), dir: t.TempDir(), open: func() {}, now: func() time.Time { mu.Lock(); defer mu.Unlock(); return now }}
	f.reopen()
	waitUntil(t, "the reopen's choice", func() bool { v, _, _, _ := s.snap(); return len(v) == 1 })
	for i := 0; i < 5; i++ {
		f.reopen()
	}
	time.Sleep(100 * time.Millisecond)
	if v, _, _, _ := s.snap(); len(v) != 1 {
		t.Errorf("five reopens inside a second showed %d choices, want 1", len(v))
	}
	mu.Lock()
	now = now.Add(2 * time.Second)
	mu.Unlock()
	f.reopen()
	waitUntil(t, "a reopen a second later", func() bool { v, _, _, _ := s.snap(); return len(v) == 2 })

	// The foreground right.
	g := newDRig(t)
	g.startInstance(g.env(g.rec), "-background")
	var granted []int
	env := g.env(&recorder{})
	env.AllowForeground = func(pid int) { granted = append(granted, pid) }
	time.Sleep(1100 * time.Millisecond)
	if _, err := g.secondStart(env); err != nil {
		t.Fatal(err)
	}
	if len(granted) != 1 || granted[0] != os.Getpid() {
		t.Errorf("the foreground right went to %v, want only the holder's pid %d", granted, os.Getpid())
	}
	// An instance file naming another pid: no grant (and no hand-off to it).
	info, _ := instance.Read(instance.FilePathIn(g.dir, ""))
	info.PID = os.Getpid() + 1
	_ = instance.Write(instance.FilePathIn(g.dir, ""), info)
	granted = nil
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	key, _ := info.KeyBytes()
	_, _ = handoffAct(ctx, g.dir, info, key, instance.ActChoose, env, time.Second, func() bool { return true })
	if len(granted) != 0 {
		t.Errorf("the foreground right went to a pid named only in the instance file: %v", granted)
	}
}

var _ = errors.New
