package desktop

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/instance"
)

// The S1 fix round 2 (docs/review-menubar-s1-adversarial-r2.md B1–B10). Every launch here
// is in-process over the real lock, listeners and hand-off; the native dialogs are
// operating-system seams (MB-18).

// quitRun runs the `quit` verb against the rig's account folder and returns its exit
// status and what it printed. The rig's copy is this test process, alive while it holds
// the lock.
func quitRun(t *testing.T, g *dRig, args ...string) (int, string) {
	return quitRunEnding(t, g, func(pid int) error {
		t.Errorf("quit ended process %d by force", pid)
		return nil
	}, args...)
}

// quitRunEnding is quitRun with the seam that ends an older copy at once.
func quitRunEnding(t *testing.T, g *dRig, end func(int) error, args ...string) (int, string) {
	t.Helper()
	var out, errb bytes.Buffer
	env := &quitEnv{
		End: end,
		Stdout: &out, Stderr: &errb,
		StateDir:  func() (string, error) { return g.dir, nil },
		Session:   g.session,
		SessionOf: func(int) (string, error) { return g.session, nil },
		Alive: func(pid int) bool {
			return pid == os.Getpid() && instance.LockIsHeld(g.dir, g.session)
		},
		Version: g.version, Reach: 2 * time.Second, Poll: 20 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	code := quitVerb(ctx, args, env)
	return code, out.String() + errb.String()
}

// covers: MA-1424, MA-1427, MA-614, R52
// `mailarchive quit` (the S1 fix round 2, B1, B2, B7): the upgrade script's way to ask a
// running copy, over the signed hand-off, never by window messages. Positive twin first:
// an idle copy asked -check says it would quit and keeps running (a question stops
// nothing); asked to quit, it quits after its own drain and the verb says so, exit 0.
// A copy running a backup is asked both and says it is busy, exit 3, naming the way to
// quit it anyway; the backup and the copy keep running. No copy is exit 2; a process that
// is not the copy is exit 2; a copy whose record lists no hand-off actions (a version
// from before the menu-bar release) cannot be asked, exit 1, and is left running — unless
// -end-older is given (the upgrade script): then -check says it would be ended and quit
// ends it, decided by its own record; -end-older never ends a copy that can be asked.
// Every code is in X1's declared set (MA-613).
func TestMA1424_TheQuitVerbStopsOnlyAnIdleCopy(t *testing.T) {
	installFrontSeams(t)
	var dash atomic.Pointer[dashboard]
	instanceDashboardMade = func(x *dashboard) { dash.Store(x) }
	t.Cleanup(func() { instanceDashboardMade = nil })

	idle := newDRig(t)
	idle.startInstance(idle.env(idle.rec), "-background")
	code, words := quitRun(t, idle, "-check")
	if code != QuitDone || !strings.Contains(words, "would quit now") || !instance.LockIsHeld(idle.dir, "") {
		t.Fatalf("twin: -check on an idle copy: %d %q, still running %v", code, words, instance.LockIsHeld(idle.dir, ""))
	}
	code, words = quitRun(t, idle, "-wait", "20s")
	if code != QuitDone || !strings.Contains(words, "has quit") {
		t.Fatalf("twin: quit on an idle copy: %d %q", code, words)
	}
	select {
	case <-idle.done:
		idle.cancel = nil
	case <-time.After(10 * time.Second):
		t.Fatal("the idle copy said it quit but kept running")
	}

	busy := newDRig(t)
	busy.startInstance(busy.env(busy.rec), "-background")
	d := dash.Load()
	finished := holdCancellable(t, d, t.TempDir(), 50*time.Millisecond)
	stillWorking := func() bool {
		select {
		case <-finished:
			return false
		default:
			return instance.LockIsHeld(busy.dir, "")
		}
	}
	for _, args := range [][]string{{"-check"}, {"-wait", "2s"}} {
		code, words := quitRun(t, busy, args...)
		assure.Refused(t, code, words, assure.Code(QuitBusy), assure.Names("is busy", "keeps running", "Shut down"),
			assure.NoSideEffect(stillWorking))
	}

	code, words = quitRun(t, busy, "-pid", "999999999")
	assure.Refused(t, code, words, assure.Code(QuitNotRunning), assure.Names("Process 999999999", "not your account's running MailArchive"),
		assure.NoSideEffect(stillWorking))

	none := newDRig(t)
	code, words = quitRun(t, none)
	assure.Refused(t, code, words, assure.Code(QuitNotRunning), assure.Names("not running for your account"))

	// A copy from before the menu-bar release: its record lists no actions.
	older := newDRig(t)
	keyHex, _ := instance.NewKey()
	if err := instance.Write(instance.FilePathIn(older.dir, ""), instance.Info{PID: os.Getpid(), Dashboard: "127.0.0.1:1", Key: keyHex, Version: "v1.7.1"}); err != nil {
		t.Fatal(err)
	}
	lock, err := instance.Acquire(older.dir, "")
	if err != nil {
		t.Fatal(err)
	}
	closeLock := sync.OnceFunc(lock.Close)
	defer closeLock()
	olderRuns := func() bool { return instance.LockIsHeld(older.dir, "") }
	code, words = quitRun(t, older)
	assure.Refused(t, code, words, assure.Code(QuitCouldNotAsk), assure.Names("older version", "Shut down"), assure.NoSideEffect(olderRuns))
	code, words = quitRun(t, older, "-check", "-end-older")
	if code != QuitDone || !strings.Contains(words, "it would be ended") || !olderRuns() {
		t.Errorf("-check -end-older on an older copy: %d %q, still running %v", code, words, olderRuns())
	}
	var ended []int
	code, words = quitRunEnding(t, older, func(pid int) error { ended = append(ended, pid); closeLock(); return nil }, "-end-older", "-wait", "5s")
	if code != QuitDone || !strings.Contains(words, "was ended") || len(ended) != 1 || ended[0] != os.Getpid() {
		t.Errorf("-end-older on an older copy: %d %q, ended %v", code, words, ended)
	}
	// -end-older never ends a copy that can be asked: the busy one still says busy.
	code, words = quitRun(t, busy, "-end-older", "-wait", "2s")
	assure.Refused(t, code, words, assure.Code(QuitBusy), assure.Names("is busy"), assure.NoSideEffect(stillWorking))
}

// covers: MA-1427, MA-1402, R50
// A first-form open from an older launcher (the S1 fix round 2, B9): it carries no
// sign-in. Positive twin first: where sign-ins are separate (Windows' lock is per
// session), it opens the copy's browser as before. Where one copy serves every sign-in of
// the account (macOS, Linux), it opens nothing on the copy's screen and gets a link, which
// an older launcher shows in its own session — never a refusal it would wait out.
func TestMA1427_AnOlderLaunchersOpenIsALinkWhereSignInsShareOneCopy(t *testing.T) {
	installFrontSeams(t)
	origSep := sessionsSeparate
	t.Cleanup(func() { sessionsSeparate = origSep })
	firstForm := func(g *dRig) instance.Reply {
		t.Helper()
		key, err := g.info.KeyBytes()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rep, err := instance.Reach(ctx, g.info.Dashboard, key, false, 3*time.Second, nil)
		if err != nil {
			t.Fatalf("an older launcher's first-form open: %v", err)
		}
		return rep
	}

	sessionsSeparate = true
	sep := newDRig(t)
	sep.startInstance(sep.env(sep.rec), "-background")
	if rep := firstForm(sep); !rep.Opened {
		t.Fatalf("twin: where sign-ins are separate the first form opened nothing: %+v", rep)
	}
	if o, _, _ := sep.rec.snapshot(); len(o) != 1 {
		t.Fatalf("twin: %d pages opened, want 1", len(o))
	}

	sessionsSeparate = false
	one := newDRig(t)
	one.startInstance(one.env(one.rec), "-background")
	rep := firstForm(one)
	if o, _, _ := one.rec.snapshot(); len(o) != 0 || rep.Opened {
		t.Errorf("an older launcher's open opened the copy's browser (%d pages, %+v)", len(o), rep)
	}
	if !strings.Contains(rep.Link, "/_open?t=") {
		t.Errorf("an older launcher's open got %+v, want a link to show in its own session", rep)
	}
}

// covers: MA-1427, R50
// The activation token leaves the environment once the choice's dialog has it (the S1 fix
// round 2, B10). Positive twin first: the dialog starts with the token in its environment,
// and while its process has not started yet the token is still there. Once the dialog
// process has started, the token is gone from this process's environment while the choice
// is still on the screen, so nothing else the copy starts meanwhile inherits it.
func TestMA1427_TheActivationTokenLeavesOnceTheDialogStarted(t *testing.T) {
	s := installFrontSeams(t)
	t.Setenv(activationEnv, "")
	os.Unsetenv(activationEnv)
	hold := make(chan struct{})
	s.mu.Lock()
	s.choiceHold = hold
	s.mu.Unlock()
	started := make(chan struct{})
	orig := dialogProcessStarted
	t.Cleanup(func() { dialogProcessStarted = orig })
	dialogProcessStarted = func(<-chan struct{}) <-chan struct{} { return started }

	f := &front{life: NewLifecycle(context.Background()), dir: t.TempDir(), session: "1", open: func() {}}
	if shown, _ := f.act(instance.ActRequest{Act: instance.ActChoose, Session: "1", Token: "tok-456"}); !shown {
		t.Fatal("the choice was not shown")
	}
	waitUntil(t, "the choice", func() bool { v, _, _, _ := s.snap(); return len(v) == 1 })
	s.mu.Lock()
	tok := s.tokens[0]
	s.mu.Unlock()
	if tok != "tok-456" {
		t.Fatalf("twin: the dialog started with token %q, want tok-456", tok)
	}
	if v := os.Getenv(activationEnv); v != "tok-456" {
		t.Fatalf("twin: before the dialog process started the token was %q", v)
	}
	close(started)
	at := time.Now()
	waitUntil(t, "the token to leave the environment", func() bool { _, set := os.LookupEnv(activationEnv); return !set })
	if d := time.Since(at); d > dialogStartBound/2 {
		t.Errorf("the token left the environment %v after the dialog process started, not at once", d)
	}
	f.mu.Lock()
	still := f.choosing
	f.mu.Unlock()
	if !still {
		t.Errorf("the choice had already closed: the token's removal was not while it showed")
	}
	close(hold)
	waitUntil(t, "the choice closed", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return !f.choosing })
}
