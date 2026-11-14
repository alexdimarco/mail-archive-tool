package desktop

import (
	"context"
	"errors"
	"flag"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/wizard"
)

// The S1 fix round (docs/review-menubar-s1-adversarial.md A1–A9, docs/review-menubar-s1-
// friction.md F1–F16). Every launch here is in-process over the real lock, listeners and
// hand-off; the native dialogs are operating-system seams (MB-18).

// covers: MA-1434, R50
// The lead's call on F2: when the choice is dismissed, or the wizard it started (or a
// hand-off's) ends, with nothing running, no wizard open and no Desktop page opened in this
// run, the program ends — this version shows no icon to say it runs (X11). Positive twin
// first: a dismissed choice after Open MailArchive Desktop leaves it running (proven in
// TestMA1434_CancelAndQuitOnTheChoice). A sign-in copy (`start -background`) whose later
// choice is dismissed ends; one whose choice ran the wizard ends when the wizard does; one
// whose wizard came by the `wizard` shortcut's hand-off ends when it does.
func TestMA1434_TheChoiceOrTheWizardEndingIdleEndsTheProgram(t *testing.T) {
	s := installFrontSeams(t)
	ends := func(name string, g *dRig) {
		t.Helper()
		select {
		case <-g.done:
		case <-time.After(20 * time.Second):
			t.Fatalf("%s: the instance kept running with nothing on the screen", name)
		}
		g.cancel = nil
		if instance.LockIsHeld(g.dir, "") {
			t.Errorf("%s: the ended instance still holds the lock", name)
		}
	}

	g := newDRig(t)
	g.startInstance(g.env(g.rec), "-background")
	if _, err := g.secondStart(nil); err != nil {
		t.Fatal(err)
	}
	ends("a later choice dismissed", g)

	s.mu.Lock()
	s.pick = pickWizard
	s.mu.Unlock()
	h := newDRig(t)
	h.startInstance(h.env(h.rec), "-background")
	if _, err := h.secondStart(nil); err != nil {
		t.Fatal(err)
	}
	ends("the wizard from the choice ended", h)
	if _, _, _, w := s.snap(); w != 1 {
		t.Errorf("the choice's wizard ran %d times, want 1", w)
	}

	k := newDRig(t)
	k.startInstance(k.env(k.rec), "-background")
	if !handOffWizard(context.Background(), k.env(&recorder{arrive: true})) {
		t.Fatal("the wizard hand-off was not handled")
	}
	ends("the handed-off wizard ended", k)
	if o, _, _ := k.rec.snapshot(); len(o) != 0 {
		t.Errorf("an idle end opened %q", o)
	}
}

// covers: MA-1403, MA-1405, R50
// The friction review's F3: while the wizard the choice started is open, a click on
// MailArchive is answered in the running copy — never only "already open" — and Desktop
// stays one click away. Positive twin first: the choice picks the wizard and it runs. A
// second start during it is told nothing in its own session and asks, in the copy, "The
// wizard is open. Open MailArchive Desktop in your browser as well?" (a question, not a
// second list: Windows' dialog library cannot open a second list beside the wizard's, as
// the fix round's VM walk found); OK opens the instance's link; Cancel opens nothing and
// runs no second wizard.
func TestMA1403_AClickDuringTheWizardReachesTheChoice(t *testing.T) {
	s := installFrontSeams(t)
	hold := make(chan struct{})
	s.hold = hold
	s.pick = pickWizard
	g := newDRig(t)
	g.startInstance(g.env(g.rec))
	waitUntil(t, "the wizard from the first choice", func() bool { _, _, _, w := s.snap(); return w == 1 })

	s.mu.Lock()
	s.pick = pickDesktop
	s.mu.Unlock()
	rec, err := g.secondStart(nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := rec.noticeList(); len(n) != 0 {
		t.Errorf("a click during the wizard was told %q, want the choice", n)
	}
	asks := func() []string { s.mu.Lock(); defer s.mu.Unlock(); return append([]string(nil), s.desktopAsks...) }
	waitUntil(t, "the question in the copy", func() bool { return len(asks()) == 1 })
	if a := asks(); a[0] != "The wizard is open. Open MailArchive Desktop in your browser as well?" {
		t.Errorf("the click during the wizard asked %q", a[0])
	}
	if v, _, _, _ := s.snap(); len(v) != 1 {
		t.Errorf("a second list was opened beside the wizard's")
	}
	waitUntil(t, "Desktop from it", func() bool { o, _, _ := g.rec.snapshot(); return len(o) == 1 })

	s.mu.Lock()
	s.pick = pickNone
	s.mu.Unlock()
	time.Sleep(1100 * time.Millisecond)
	if _, err := g.secondStart(nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the second question", func() bool { return len(asks()) == 2 })
	time.Sleep(200 * time.Millisecond)
	if o, _, _ := g.rec.snapshot(); len(o) != 1 {
		t.Errorf("Cancel on the question opened Desktop")
	}
	if _, _, _, w := s.snap(); w != 1 {
		t.Errorf("%d wizards ran, want 1", w)
	}
	close(hold)
	waitUntil(t, "the wizard let go", func() bool { return !wizard.OpenHere() })
}

// covers: MA-1422, MA-1434, MA-1424, R52
// An open wizard is work (the adversarial pass A4, the friction review's F5 and F9).
// Positive twin first: with the first choice on the screen and nothing else open, its
// dismissal ends the program. With the `wizard` shortcut's wizard open in the instance:
// the first choice's dismissal leaves it running; the choice's "Now:" line, Desktop's
// Shut down and the choice's Quit all ask "The wizard is open. Quit anyway? …", Keep
// running keeping it; once the wizard ends, the program ends (F2).
func TestMA1422_AnOpenWizardIsWork(t *testing.T) {
	s := installFrontSeams(t)
	g0 := newDRig(t)
	g0.startInstance(g0.env(g0.rec))
	select {
	case <-g0.done:
		g0.cancel = nil
	case <-time.After(20 * time.Second):
		t.Fatalf("twin: the first choice's dismissal with nothing open did not end the program")
	}

	firstChoice := make(chan struct{})
	s.mu.Lock()
	s.choiceHold = firstChoice
	s.mu.Unlock()
	wizardOpen := make(chan struct{})
	s.hold = wizardOpen
	var dash atomic.Pointer[dashboard]
	instanceDashboardMade = func(x *dashboard) { dash.Store(x) }
	t.Cleanup(func() { instanceDashboardMade = nil })
	g := newDRig(t)
	g.startInstance(g.env(g.rec))
	waitUntil(t, "the first choice", func() bool { v, _, _, _ := s.snap(); return len(v) == 2 })
	if !handOffWizard(context.Background(), g.env(&recorder{arrive: true})) {
		t.Fatal("the wizard hand-off was not handled")
	}
	waitUntil(t, "the handed-off wizard", func() bool { _, _, _, w := s.snap(); return w == 1 })
	s.mu.Lock()
	s.choiceHold = nil
	s.mu.Unlock()
	close(firstChoice) // the person dismisses the first choice
	time.Sleep(300 * time.Millisecond)
	if !instance.LockIsHeld(g.dir, "") {
		t.Fatalf("the first choice's dismissal stopped the program while the wizard was open")
	}
	d := dash.Load()
	if q := d.quitQuestion(); q != quitWizOpenText || !strings.HasPrefix(q, "The wizard is open. Quit anyway?") {
		t.Errorf("the Quit question while the wizard is open: %q", q)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "http://127.0.0.1:8097/api/shutdown?check=1", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	req.Header.Set("X-CSRF-Token", d.csrf)
	d.shutdown(w, req)
	if body := w.Body.String(); !strings.Contains(body, `"ask":true`) || !strings.Contains(body, "The wizard is open.") {
		t.Errorf("Shut down while the wizard is open: %s", body)
	}
	// The choice's Quit (MB-14's item) asks too; Keep running keeps it.
	f := &front{life: d.life, question: d.quitQuestion}
	s.mu.Lock()
	s.quit = false
	s.mu.Unlock()
	f.personQuits()
	if _, _, q, _ := s.snap(); len(q) != 1 || q[0] != quitWizOpenText {
		t.Errorf("Quit while the wizard is open asked %q", q)
	}
	if d.life.Context().Err() != nil {
		t.Fatalf("Keep running stopped the program")
	}
	close(wizardOpen)
	select {
	case <-g.done:
		g.cancel = nil
	case <-time.After(20 * time.Second):
		t.Fatalf("the program kept running after the wizard ended with nothing opened")
	}
}

// covers: MA-1403, MA-1424, R50, R52
// A copy that is closing (the adversarial pass A7). Positive twin first: a copy that
// runs answers a reopen and a hand-off. Once its stop begins (a backup held in the slot
// takes 3 s to reach its safe point): a reopen shows nothing, a hand-off is answered
// "closing", its instance file says it is closing, and a launch made during the drain —
// longer than the launch's own 2 s wait — waits for it to end and becomes the next copy,
// never telling the person to kill the one that protects the work.
func TestMA1403_ACopyThatIsClosingShowsNothingAndALaunchWaits(t *testing.T) {
	s := installFrontSeams(t)
	s.pick = pickDesktop
	var dash atomic.Pointer[dashboard]
	instanceDashboardMade = func(x *dashboard) { dash.Store(x) }
	t.Cleanup(func() { instanceDashboardMade = nil })
	g := newDRig(t)
	g.startInstance(g.env(g.rec), "-background")
	if _, err := g.secondStart(nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the running copy's choice", func() bool { v, _, _, _ := s.snap(); return len(v) == 1 })

	d := dash.Load()
	finished := holdCancellable(t, d, t.TempDir(), 3*time.Second)
	d.life.Stop()
	waitUntil(t, "the closing mark", func() bool {
		i, err := instance.Read(instance.FilePathIn(g.dir, ""))
		return err == nil && i.Closing
	})
	f := &front{life: d.life, open: func() { t.Errorf("a closing copy opened Desktop") }, session: "41"}
	f.reopen()
	if shown, reason := f.act(instance.ActRequest{Act: instance.ActChoose, Session: "41"}); shown || reason != instance.ReasonClosing {
		t.Errorf("a hand-off to a closing copy: %v %q, want closing", shown, reason)
	}
	time.Sleep(200 * time.Millisecond)
	if v, _, _, _ := s.snap(); len(v) != 1 {
		t.Errorf("a closing copy showed the choice")
	}

	// A launch during the drain.
	next := newDRig(t)
	next.dir, next.cfgPath, next.out = g.dir, g.cfgPath, g.out
	rec := &recorder{arrive: true}
	env := next.env(rec)
	ready := make(chan instance.Info, 1)
	env.Ready = func(i instance.Info) { ready <- i }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- launchStart(ctx, next.args("-background"), env) }()
	select {
	case i := <-ready:
		select {
		case <-finished:
		default:
			t.Errorf("the next copy started before the closing one's work ended")
		}
		if i.Closing {
			t.Errorf("the next copy's file says closing")
		}
	case err := <-done:
		t.Fatalf("the launch during the drain ended: %v (refusals %v, notices %v)", err, rec.refusals, rec.noticeList())
	case <-time.After(30 * time.Second):
		t.Fatalf("the launch during the drain never became the next copy")
	}
	if _, r, _ := rec.snapshot(); len(r) != 0 {
		t.Errorf("the launch during the drain refused %q", r)
	}
	cancel()
	<-done
}

// covers: MA-1403, MA-1424, R50, R52
// A copy still closing at the end of a launch's whole wait (the adversarial pass A7):
// the launch says MailArchive is finishing its work and closing, and never refuses with
// "not responding … kill it". Positive twin first: the copy's file says it is closing.
// (The wait is shortened here; the real one is the drain's bound plus 10 s.)
func TestMA1403_ALaunchThatOutwaitsAClosingCopySaysSo(t *testing.T) {
	installFrontSeams(t)
	orig := closingWait
	closingWait = 500 * time.Millisecond
	t.Cleanup(func() { closingWait = orig })
	var dash atomic.Pointer[dashboard]
	instanceDashboardMade = func(x *dashboard) { dash.Store(x) }
	t.Cleanup(func() { instanceDashboardMade = nil })
	g := newDRig(t)
	g.startInstance(g.env(g.rec), "-background")
	d := dash.Load()
	finished := holdCancellable(t, d, t.TempDir(), 4*time.Second)
	d.life.Stop()
	waitUntil(t, "the closing mark", func() bool {
		i, err := instance.Read(instance.FilePathIn(g.dir, ""))
		return err == nil && i.Closing
	})
	rec := &recorder{arrive: true}
	env := g.env(rec)
	env.Window = 200 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := launchStart(ctx, g.args(), env); err != nil {
		t.Fatalf("the launch: %v", err)
	}
	if n := rec.noticeList(); len(n) != 1 || n[0] != closingText {
		t.Errorf("the launch was told %q, want %q", n, closingText)
	}
	if _, r, _ := rec.snapshot(); len(r) != 0 {
		t.Errorf("the launch refused %q", r)
	}
	<-finished
}

// covers: MA-1427, MA-1402, R50, R23
// R50 for every launch (the adversarial pass A3): the `desktop` verb's open carries the
// launch's sign-in. Positive twin first: a `desktop` launch of the copy's own sign-in opens
// Desktop in its browser. A `desktop` launch from another sign-in opens nothing on the
// copy's screen and is told R50's line in its own session; one whose sign-in cannot be
// read ("") opens nothing and is told it cannot tell (fail closed); a `start` whose
// sign-in cannot be read shows no choice there. A `-copy-link` launch from another sign-in
// still gets its link to show in its own session (nothing opens on the other screen).
func TestMA1427_ALaunchFromAnotherOrUnknownSignInOpensNothingThere(t *testing.T) {
	s := installFrontSeams(t)
	origSep := sessionsSeparate
	sessionsSeparate = false
	t.Cleanup(func() { sessionsSeparate = origSep })
	g := newDRig(t)
	g.guiSession = "100"
	g.startInstance(g.env(g.rec), "-background")

	run := func(sess string, extra ...string) *recorder {
		t.Helper()
		time.Sleep(1100 * time.Millisecond)
		rec := &recorder{arrive: true}
		env := g.env(rec)
		env.GUISession = sess
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := launch(ctx, g.args(extra...), env); err != nil {
			t.Fatalf("a desktop launch from sign-in %q: %v", sess, err)
		}
		return rec
	}
	run("100")
	if o, _, _ := g.rec.snapshot(); len(o) != 1 {
		t.Fatalf("twin: a desktop launch of the same sign-in opened %d pages, want 1", len(o))
	}
	other := run("200")
	unknown := run("")
	if o, _, _ := g.rec.snapshot(); len(o) != 1 {
		t.Errorf("a launch from another or an unknown sign-in opened the copy's browser (%d pages)", len(o))
	}
	if n := other.noticeList(); len(n) != 1 || n[0] != otherSessionText {
		t.Errorf("the other sign-in was told %q", n)
	}
	if n := unknown.noticeList(); len(n) != 1 || n[0] != unknownSessionText {
		t.Errorf("the unknown sign-in was told %q", n)
	}
	link := run("200", "-copy-link")
	if _, _, l := link.snapshot(); len(l) != 1 || !strings.Contains(l[0], "/_open?t=") {
		t.Errorf("a -copy-link launch from another sign-in got %q, want its link", l)
	}
	time.Sleep(1100 * time.Millisecond)
	st := g.env(&recorder{})
	st.GUISession = ""
	rec, err := g.secondStart(st)
	if err != nil {
		t.Fatal(err)
	}
	if v, _, _, _ := s.snap(); len(v) != 0 {
		t.Errorf("a start whose sign-in cannot be read showed the choice on the copy's screen")
	}
	if n := rec.noticeList(); len(n) != 1 || n[0] != unknownSessionText {
		t.Errorf("that start was told %q", n)
	}
	if o, _, _ := g.rec.snapshot(); len(o) != 1 {
		t.Errorf("that start opened the copy's browser")
	}
}

// covers: MA-1427, R50
// The launch's Wayland activation token reaches the choice (the adversarial pass A9):
// the dialog process starts with it in its environment, and it is gone after. Positive
// twin first: a choose with no token shows the choice with none.
func TestMA1427_TheActivationTokenReachesTheChoice(t *testing.T) {
	s := installFrontSeams(t)
	t.Setenv(activationEnv, "")
	os.Unsetenv(activationEnv)
	f := &front{life: NewLifecycle(context.Background()), dir: t.TempDir(), session: "1", open: func() {}}
	if shown, _ := f.act(instance.ActRequest{Act: instance.ActChoose, Session: "1"}); !shown {
		t.Fatal("the choice was not shown")
	}
	waitUntil(t, "the first choice", func() bool { v, _, _, _ := s.snap(); return len(v) == 1 })
	waitUntil(t, "the first choice closed", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return !f.choosing })
	if shown, _ := f.act(instance.ActRequest{Act: instance.ActChoose, Session: "1", Token: "tok-123"}); !shown {
		t.Fatal("the second choice was not shown")
	}
	waitUntil(t, "the second choice", func() bool { v, _, _, _ := s.snap(); return len(v) == 2 })
	waitUntil(t, "the second choice closed", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return !f.choosing })
	s.mu.Lock()
	tokens := append([]string(nil), s.tokens...)
	s.mu.Unlock()
	if tokens[0] != "" || tokens[1] != "tok-123" {
		t.Errorf("the tokens the choices saw: %q, want none then tok-123", tokens)
	}
	if _, set := os.LookupEnv(activationEnv); set {
		t.Errorf("the token stayed in the environment after the choice")
	}
}

// covers: MA-1403, R50
// The default item counts every archive (the friction review, F8). Positive twin first:
// with no archive anywhere, the wizard is the default. An archive in the known-archives
// list, or the one the wizard made last, makes Desktop the default even before Desktop's
// current archive is set.
func TestMA1403_TheDefaultItemCountsEveryArchive(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{SettingsPath: filepath.Join(dir, "s.json"), IndexPath: filepath.Join(dir, "archives.json")}
	f := &front{hasArchives: cfg.hasAnyArchive}
	if v := f.view(false); v.DefaultItem != choiceWizardItem {
		t.Fatalf("twin: no archive anywhere, default %q", v.DefaultItem)
	}
	idx := archindex.Index{}
	idx.Entries = append(idx.Entries, archindex.Entry{Path: filepath.Join(dir, "Mail")})
	if err := archindex.WriteFile(cfg.IndexPath, idx); err != nil {
		t.Fatal(err)
	}
	if v := f.view(false); v.DefaultItem != choiceDesktopItem {
		t.Errorf("an archive in the list: default %q, want Desktop", v.DefaultItem)
	}
	cfg2 := Config{SettingsPath: filepath.Join(dir, "s2.json"), LastArchive: func() string { return filepath.Join(dir, "Made") }}
	if !cfg2.hasAnyArchive() {
		t.Errorf("the wizard's last archive does not count")
	}
}

// covers: MA-1431, MA-1405, MA-1427, R50
// The words (the friction review's F1, F6, F16, and R50's wording). Positive twin first:
// the question is the prompt when nothing else is said. Every item fits Windows' 241-pixel
// list (under 41 characters) and every prompt is one line (Windows shows one); the newer
// line is the prompt alone, the Quit item says what it is for, the "Now:" line names the
// work; only Windows' notices name a taskbar.
func TestMA1431_TheChoiceWordsFitOneLine(t *testing.T) {
	if (choiceView{}).Text() != "What would you like to do?" {
		t.Fatalf("twin: the prompt with nothing else to say is %q", (choiceView{}).Text())
	}
	for _, item := range []string{choiceWizardItem, choiceDesktopItem, choiceQuitItem} {
		if n := utf8.RuneCountInString(item); n > 40 {
			t.Errorf("item %q has %d characters, over 40", item, n)
		}
	}
	if choiceWizardItem != "Add or back up mail, step by step" || choiceDesktopItem != "Open MailArchive Desktop in your browser" || choiceQuitItem != "Quit, to use the new version" {
		t.Errorf("the items are %q, %q, %q", choiceWizardItem, choiceDesktopItem, choiceQuitItem)
	}
	f := &front{task: func() string { return taskBackup }}
	busy := f.view(false)
	newer := f.view(true)
	for _, v := range []choiceView{busy, newer} {
		if strings.Contains(v.Text(), "\n") || utf8.RuneCountInString(v.Text()) > 40 {
			t.Errorf("the prompt %q is not one short line", v.Text())
		}
	}
	if busy.Text() != "Now: a backup is running." || newer.Text() != "A newer MailArchive is installed." || !newer.Quit {
		t.Errorf("the prompts: %q and %q (quit %v)", busy.Text(), newer.Text(), newer.Quit)
	}
	if strings.Contains(choiceOpenTextOn("darwin"), "taskbar") || strings.Contains(choiceOpenTextOn("linux"), "taskbar") ||
		strings.Contains(instance.WizardOpenTextOn("darwin"), "taskbar") || !strings.Contains(choiceOpenTextOn("windows"), "taskbar") ||
		!strings.Contains(instance.WizardOpenTextOn("windows"), "taskbar") {
		t.Errorf("a notice names the taskbar where there is none, or not on Windows")
	}
	if otherSessionText != "MailArchive is already open for this account on another screen (another sign-in). Use it there, or quit it there to open it here." {
		t.Errorf("R50's line is %q", otherSessionText)
	}
	if quitBackupText != "A backup is running. Quit anyway? The backup stops; the next backup picks up where it left off." {
		t.Errorf("the backup's Quit question is %q", quitBackupText)
	}
	_ = wizard.OpenHere
	// F11: the choice's list needs a picked item for OK (zenity.DisallowEmpty), so OK with
	// nothing picked is never read as Cancel; and it closes with the instance (zenity.Context).
	// The native list is an operating-system seam, so this is read from the source.
	src, err := os.ReadFile("front_dialogs.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if i := strings.Index(body, "func zenityChoose("); i < 0 {
		t.Errorf("zenityChoose is gone")
	} else if fn := body[i:]; !strings.Contains(fn[:strings.Index(fn, "\n}\n")], "zenity.DisallowEmpty()") ||
		!strings.Contains(fn[:strings.Index(fn, "\n}\n")], "zenity.Context(ctx)") {
		t.Errorf("the choice's list allows an empty pick or outlives the instance")
	}
}

// covers: MA-1422, R52
// Shut down asks at the first click while work runs (the friction review, F4). Positive
// twin first: idle, check=1 answers no question and stops nothing. With a job in the slot
// it answers the question and stops nothing; Quit (confirm=1) once the job has ended
// says nothing was running any more. The page side runs under Node.
func TestMA1422_ShutDownAsksAtTheFirstClick(t *testing.T) {
	post := func(d *dashboard, url string) string {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", url, nil)
		req.Header.Set("Origin", "http://127.0.0.1:8097")
		req.Header.Set("X-CSRF-Token", d.csrf)
		d.shutdown(w, req)
		return w.Body.String()
	}
	life := NewLifecycle(context.Background())
	d := newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Lifecycle: life})
	if body := post(d, "http://127.0.0.1:8097/api/shutdown?check=1"); !strings.Contains(body, `"ask":false`) || life.Context().Err() != nil {
		t.Fatalf("twin: check=1 idle: %s (stopped %v)", body, life.Context().Err() != nil)
	}
	finished := holdCancellable(t, d, t.TempDir(), 0)
	if body := post(d, "http://127.0.0.1:8097/api/shutdown?check=1"); !strings.Contains(body, `"ask":true`) || !strings.Contains(body, "A task is running. Quit anyway?") || life.Context().Err() != nil {
		t.Errorf("check=1 with a job: %s (stopped %v)", body, life.Context().Err() != nil)
	}
	d.cap.mu.Lock()
	cancelJob := d.cap.cancel
	d.cap.mu.Unlock()
	cancelJob(context.Canceled)
	<-finished
	waitUntil(t, "the job ended", func() bool { return d.quitQuestion() == "" })
	if body := post(d, "http://127.0.0.1:8097/api/shutdown?confirm=1"); !strings.Contains(body, `"ok":true`) || !strings.Contains(body, shutdownNothingRanText) {
		t.Errorf("Quit after the job ended: %s", body)
	}
	if passed := runPageScenario(t, "shutdownAsks"); len(passed) != 5 {
		t.Errorf("the shutdownAsks scenario passed %d of its 5 checks: %v", len(passed), passed)
	}
}

// covers: MA-1400, R50
// The `start` verb's terminal words (the friction review, F14). Positive twin first:
// `start -h` is help (flag.ErrHelp) and lists its flags. It first says what `start` is;
// an argument is refused in words that the dispatcher's "mailarchive: " does not double;
// where no dialog can show, the log never says a dialog shows the link.
func TestMA1400_TheStartVerbsTerminalWords(t *testing.T) {
	g := newDRig(t)
	var out strings.Builder
	env := g.env(&recorder{})
	env.Stderr = &out
	err := launchStart(context.Background(), []string{"-h"}, env)
	if !errors.Is(err, flag.ErrHelp) || !strings.Contains(out.String(), "-background") {
		t.Fatalf("start -h: %v, %q", err, out.String())
	}
	if !strings.HasPrefix(out.String(), "Usage: mailarchive start [flags]\n\nOpens MailArchive as its app icon does") {
		t.Errorf("start -h does not first say what start is: %q", out.String())
	}
	err = launchStart(context.Background(), []string{"foo"}, env)
	if err == nil || !strings.HasPrefix(err.Error(), "start takes no arguments, only flags (got \"foo\")") {
		t.Errorf("start foo: %v", err)
	}
	orig := canShowDialogs
	canShowDialogs = func() bool { return false }
	t.Cleanup(func() { canShowDialogs = orig })
	r, w, perr := os.Pipe()
	if perr != nil {
		t.Fatal(perr)
	}
	saved := os.Stderr
	os.Stderr = w
	showLink("http://127.0.0.1:1/_open?t=x")
	os.Stderr = saved
	w.Close()
	logged, _ := io.ReadAll(r)
	if string(logged) != noLinkDialogText+"\n" || strings.Contains(string(logged), "_open") {
		t.Errorf("with no dialog possible the log said %q", logged)
	}
}
