package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/presyncjob"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// The plaintext Run now with the Thunderbird sync (design-presync-attended §3.1, §4.6,
// §7, slice AS4; owner rulings OQ-1, OQ-2, OQ-6, OQ-7). Thunderbird is the external
// binary, so the session is a scripted fake through presyncjob's seam (C-41, C-79) —
// the mock CLAUDE.md allows; the rest is real: the Desktop's handlers, the wizard's
// RunJobInProcess and app.Run over a real archive. No real Thunderbird is started,
// and the home and config folders are the test's own.

// tbScript is the scripted Thunderbird session. It reports `starting`, then (unless
// it holds in starting) `waiting`, and then ends by whichever comes first: the run's
// context (a Cancel: left open by rule when its cause is the Cancel button's, else
// left open), the Stop (Shut down: left open), EndWait ("Stop waiting": wait-ended),
// or a release (the mail settled: ok, left open by rule). It records each call's
// whole Config, the context's cause and whether the Stop had ended.
type tbScript struct {
	mu             sync.Mutex
	cfgs           []presync.Config
	causes         []error
	stopEnded      []bool
	at             chan string
	release        chan struct{}
	holdStart      bool
	waitedFor      time.Duration
	panics         bool
	minimized      bool
	cancelOnReturn func()
	// onStop, when set, is called while the session still runs, as its Stop ends in
	// the wait (a Lock): the encrypted Run now's key is checked there (MA-913).
	onStop func()
}

func newTBScript() *tbScript {
	return &tbScript{at: make(chan string, 32), release: make(chan struct{}, 1)}
}

func (s *tbScript) run(ctx context.Context, cfg presync.Config) presync.Result {
	s.mu.Lock()
	s.cfgs = append(s.cfgs, cfg)
	panics, hold, waited, minimized, onReturn, onStop := s.panics, s.holdStart, s.waitedFor, s.minimized, s.cancelOnReturn, s.onStop
	s.mu.Unlock()
	if panics {
		panic("a scripted session failure")
	}
	stop := cfg.Stop
	if stop == nil {
		stop = context.Background()
	}
	var endDone <-chan struct{}
	if cfg.EndWait != nil {
		endDone = cfg.EndWait.Done()
	}
	phase := func(p string, since time.Time) {
		if cfg.Phase != nil {
			cfg.Phase(p, since)
		}
		s.at <- p
	}
	record := func() {
		s.mu.Lock()
		s.causes = append(s.causes, context.Cause(ctx))
		s.stopEnded = append(s.stopEnded, stop.Err() != nil)
		s.mu.Unlock()
	}
	started := time.Now()
	phase(presync.PhaseStarting, started)
	if hold {
		select {
		case <-ctx.Done():
		case <-stop.Done():
		}
		record()
		if stop.Err() == nil && errors.Is(context.Cause(ctx), presync.ErrCancelAndQuit) {
			phase(presync.PhaseCancelling, time.Now()) // the S6 child poll (C-74)
			<-s.release
		}
		return presync.Result{Outcome: presync.OutcomeCancelled, Left: presync.LeftMayBeStarting, Started: started}
	}
	phase(presync.PhaseWaiting, time.Now().Add(-waited))
	select {
	case <-ctx.Done():
		if onStop != nil && stop.Err() != nil {
			onStop()
		}
		record()
		left := presync.LeftOpen
		if stop.Err() == nil && errors.Is(context.Cause(ctx), presync.ErrCancelAndQuit) {
			left = presync.LeftOpenByRule
		}
		return presync.Result{Outcome: presync.OutcomeCancelled, Left: left, Started: started, Waited: waited, Minimized: minimized}
	case <-stop.Done():
		if onStop != nil {
			onStop()
		}
		record()
		return presync.Result{Outcome: presync.OutcomeCancelled, Left: presync.LeftOpen, Started: started}
	case <-endDone:
		record()
		return presync.Result{Outcome: presync.OutcomeWaitEnded, Left: presync.LeftOpenByRule, Started: started, Waited: waited, Minimized: minimized}
	case <-s.release:
	}
	if onReturn != nil {
		onReturn()
	}
	return presync.Result{Outcome: presync.OutcomeOK, Verdict: presync.OutcomeOK, Left: presync.LeftOpenByRule,
		Started: started, Waited: 6 * time.Minute, Minimized: minimized}
}

func (s *tbScript) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cfgs)
}

func (s *tbScript) last() (presync.Config, error, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cause error
	var stopped bool
	if n := len(s.causes); n > 0 {
		cause, stopped = s.causes[n-1], s.stopEnded[n-1]
	}
	return s.cfgs[len(s.cfgs)-1], cause, stopped
}

// runNowRig is a Desktop for a plaintext archive made from a Thunderbird profile, with
// its run-job (the sync on) in the test's own config folder and the scripted session.
type runNowRig struct {
	t    *testing.T
	d    *dashboard
	out  string
	jp   string
	life *Lifecycle
	s    *tbScript
	dir  string // the pre-sync state folder the seam hands the session
	goos string
}

func newRunNowRig(t *testing.T, goos string, startAllowed bool) *runNowRig {
	t.Helper()
	isolateHome(t)
	out := tbProfileArchive(t)
	name := schedule.DefaultNameFor(out)
	jp, err := job.PathFor(name)
	if err != nil {
		t.Fatal(err)
	}
	jj, ok := reconstructLocalJob(out)
	if !ok {
		t.Fatal("fixture: the archive cannot be reconstructed into a job")
	}
	jj.Name, jj.ThunderbirdSync = name, &job.ThunderbirdSync{}
	if err := job.Write(jp, jj); err != nil {
		t.Fatal(err)
	}
	g := &runNowRig{t: t, out: out, jp: jp, s: newTBScript(), dir: filepath.Join(t.TempDir(), "presync"), goos: goos}
	t.Cleanup(presyncjob.OverrideSessionForTest(g.s.run, g.dir))
	t.Cleanup(presync.OverrideTablesForTest(nil, map[string]bool{goos: startAllowed}))
	g.life = NewLifecycle(context.Background())
	t.Cleanup(g.life.Stop)
	settings := filepath.Join(t.TempDir(), "s.json")
	SaveSettings(settings, Settings{Out: out})
	g.d = newDashboard(Config{Out: out, GOOS: goos, SettingsPath: settings, Lifecycle: g.life})
	return g
}

func (g *runNowRig) activity() map[string]any {
	rec := httptest.NewRecorder()
	g.d.activity(rec, httptest.NewRequest(http.MethodGet, "/api/activity", nil))
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return m
}

func (g *runNowRig) until(what string, ok func(map[string]any) bool) map[string]any {
	g.t.Helper()
	for i := 0; i < 1500; i++ {
		if m := g.activity(); ok(m) {
			return m
		}
		time.Sleep(20 * time.Millisecond)
	}
	g.t.Fatalf("never: %s (activity %v)", what, g.activity())
	return nil
}

// start clicks Run now.
func (g *runNowRig) start() {
	g.t.Helper()
	if m := callAction(g.t, g.d, g.d.runBackup, nil); m["ok"] != true {
		g.t.Fatalf("Run now did not start: %v", m)
	}
}

// at waits for the script to reach a phase.
func (g *runNowRig) at(phase string) {
	g.t.Helper()
	for {
		select {
		case p := <-g.s.at:
			if p == phase {
				return
			}
		case <-time.After(30 * time.Second):
			g.t.Fatalf("the session never reached %q", phase)
		}
	}
}

// done waits for the Run now to end and returns its activity.
func (g *runNowRig) done() map[string]any {
	g.t.Helper()
	return g.until("the Run now ends", func(m map[string]any) bool { return m["running"] != true })
}

func (g *runNowRig) lastRunStatus() string {
	lr, _, err := state.ReadLastRun(g.out)
	if err != nil {
		g.t.Fatal(err)
	}
	return lr.Status
}

// covers: MA-914, MA-922
// The plaintext Run now is cancellable, with causes (design-presync-attended §3.1,
// C-23, C-41/T15, C-64, OQ-1). Positive twin first: a Run now whose sync waits offers
// Cancel, and released it syncs and backs up. Cancel delivers context.Cause =
// presync.ErrCancelAndQuit to the session, with its Stop not ended; the whole backup
// is skipped (the record reads cancelled) and the end note says Thunderbird, started
// by this backup, was left open, where to quit it, and that the archive is unchanged
// — never that it was closed (never-close). A Cancel while Thunderbird is still
// starting shows the S6 `cancelling` note, then "may still be starting". On Windows
// with the minimize never done, its window may still be on screen. Shut down ends the
// Stop and delivers another cause. A Cancel once the sync has ended (reading) keeps
// today's words, after the sync's line, and the archive is intact.
func TestRunNowCancelCauses(t *testing.T) {
	g := newRunNowRig(t, "darwin", true)
	g.start()
	g.at(presync.PhaseWaiting)
	m := g.activity()
	if m["cancellable"] != true || m["runNow"] != true {
		t.Fatalf("twin: a Run now waiting for Thunderbird offers Cancel: %v", m)
	}
	g.s.release <- struct{}{}
	m = g.done()
	if m["error"] != "" || !strings.Contains(str(m["note"]), "Thunderbird synced: its mail settled after 6 min.") || g.lastRunStatus() != state.RunOK {
		t.Fatalf("twin: the released Run now: %v, record %s", m, g.lastRunStatus())
	}

	// Cancel while it waits.
	g.start()
	g.at(presync.PhaseWaiting)
	if c := callAction(t, g.d, g.d.cancelJobAction, nil); c["ok"] != true {
		t.Fatalf("cancel: %v", c)
	}
	m = g.done()
	_, cause, stopped := g.s.last()
	if !errors.Is(cause, presync.ErrCancelAndQuit) || stopped {
		t.Errorf("Cancel delivered cause %v, Stop ended %v; want ErrCancelAndQuit and the Stop live", cause, stopped)
	}
	want := "Cancelled. Thunderbird, started by this backup, was left open out of sight; quit it from the Dock. The archive is unchanged."
	if str(m["note"]) != want || str(m["result"]) != want || m["error"] != "" {
		t.Errorf("the cancel's end note %q (result %q, error %q), want %q", m["note"], m["result"], m["error"], want)
	}
	if strings.Contains(str(m["note"]), "closed") || g.lastRunStatus() != state.RunCancelled {
		t.Errorf("never-close: %q; the record reads %s, want cancelled", m["note"], g.lastRunStatus())
	}
	if lg := strings.Join(logLines(m["log"]), "\n"); !strings.Contains(lg, "the whole backup is skipped") {
		t.Errorf("the Cancel's line does not say the whole backup is skipped: %s", lg)
	}

	// Cancel while Thunderbird is still starting: the S6 child poll's note, then "may
	// still be starting".
	g.s.holdStart = true
	g.start()
	g.at(presync.PhaseStarting)
	callAction(t, g.d, g.d.cancelJobAction, nil)
	g.at(presync.PhaseCancelling)
	if n := str(g.activity()["syncNote"]); n != "Cancelling… Thunderbird is still starting; it will be left open." {
		t.Errorf("the S6 cancelling note: %q", n)
	}
	g.s.release <- struct{}{}
	if n := str(g.done()["note"]); n != "Cancelled. Thunderbird may still be starting; quit it from the Dock." {
		t.Errorf("a cancel while starting: %q", n)
	}
	g.s.holdStart = false

	// A Cancel once the sync has ended: today's words after the sync's line, and the
	// archive intact.
	g.s.cancelOnReturn = func() { callAction(t, g.d, g.d.cancelJobAction, nil) }
	g.start()
	g.at(presync.PhaseWaiting)
	g.s.release <- struct{}{}
	m = g.done()
	g.s.cancelOnReturn = nil
	if n := str(m["note"]); !strings.HasPrefix(n, "Thunderbird synced: its mail settled after 6 min.") || !strings.HasSuffix(n, cancelledText) {
		t.Errorf("a cancel while reading: %q, want the sync's line then today's words", n)
	}
	rep, err := app.Verify(g.out, app.VerifyOptions{}, log.New(io.Discard, "", 0), nil)
	if err != nil || rep.Modified != 0 || rep.Missing != 0 || g.lastRunStatus() != state.RunCancelled {
		t.Errorf("after a cancel while reading: verify %+v (%v), record %s; want the archive intact, cancelled", rep, err, g.lastRunStatus())
	}

	// Windows, the minimize never done: its window may still be on screen.
	w := newRunNowRig(t, "windows", true)
	w.start()
	w.at(presync.PhaseWaiting)
	callAction(t, w.d, w.d.cancelJobAction, nil)
	if n := str(w.done()["note"]); n != "Cancelled. Thunderbird, started by this backup, was left open; its window may still be on screen; quit it from the taskbar or the notification area. The archive is unchanged." {
		t.Errorf("Windows, not minimized: %q", n)
	}

	// Shut down: the Stop ends, and the cause is not the Cancel button's.
	sd := newRunNowRig(t, "darwin", true)
	sd.start()
	sd.at(presync.PhaseWaiting)
	sd.life.Stop()
	sd.done()
	if _, cause, stopped := sd.s.last(); errors.Is(cause, presync.ErrCancelAndQuit) || !stopped {
		t.Errorf("Shut down delivered cause %v, Stop ended %v; want another cause and the Stop ended", cause, stopped)
	}
}

// logLines is a reply's list of lines as text (nil when absent).
func logLines(v any) []string {
	var out []string
	if l, ok := v.([]any); ok {
		for _, x := range l {
			out = append(out, str(x))
		}
	}
	return out
}

// covers: MA-906, MA-922
// The Run now note is built on the server (design-presync-attended §4.6, C-11, C-15,
// C-19, C-22, C-49, C-50, C-69, C-71, C-83, OQ-6). /api/activity carries syncPhase
// and the note for each phase AS4 builds — starting, waiting (the minute count,
// never above 20; the 13-to-20-minute sentence; the Stop-waiting sentence) — then the
// sync's line with the result. No phase is `closing` and no text offers "Keep
// Thunderbird open" (never-close until AS-Q). A new job resets the sync fields with
// the lines and the result. A late Phase call from an earlier Run now's session, or
// one carrying another slot id, does not change the running Run now's note. A
// session that panics leaves no phase, and its line is said. Where the platform's
// attended start is not allowed, no session runs and the note says why, with no
// outcome recorded. The page re-attaches the Run now poll after a reload. A Lock of
// an encrypted archive, or a switch away from it, clears its job's activity.
func TestRunNowNoteBuiltOnTheServer(t *testing.T) {
	g := newRunNowRig(t, "darwin", true)
	g.s.waitedFor = 25 * time.Minute
	g.start()
	g.at(presync.PhaseStarting)
	g.at(presync.PhaseWaiting)
	m := g.activity()
	note := str(m["syncNote"])
	if m["syncPhase"] != presync.PhaseWaiting || !strings.Contains(note, "(20 min so far, up to 20)") ||
		!strings.Contains(note, "Thunderbird may fetch at once, or wait about 10 minutes for its regular mail check, so this often takes about 13 minutes; with no new mail it takes the full 20.") ||
		!strings.Contains(note, "Thunderbird is running out of sight and stays open after the wait.") ||
		!strings.Contains(note, "click Stop waiting and back up now") {
		t.Errorf("the waiting note (25 min in): phase %v, %q", m["syncPhase"], note)
	}
	if !strings.Contains(str(m["cancelTitle"]), "the whole backup is skipped, not only the wait") {
		t.Errorf("Cancel's sync tooltip: %q", m["cancelTitle"])
	}
	firstCfg, _, _ := g.s.last()
	g.s.release <- struct{}{}
	m = g.done()
	end := str(m["note"])
	if !strings.HasPrefix(end, "Thunderbird synced: its mail settled after 6 min. It was left open out of sight; quit it from the Dock when you are done. ") ||
		!strings.Contains(end, "Export complete.") || m["syncPhase"] != "" {
		t.Errorf("the end note: %q (phase %v)", end, m["syncPhase"])
	}
	// A failed run no longer hides the sync's line (C-19).
	if f := runNowEndNote("Thunderbird synced: x.", "ok", "", "the disk is full"); f != "Thunderbird synced: x. Backup failed: the disk is full" {
		t.Errorf("the failure note: %q", f)
	}
	for _, s := range []string{note, end, str(m["cancelTitle"])} {
		if strings.Contains(s, "Keep Thunderbird open") || strings.Contains(s, "Closing") || strings.Contains(s, "was closed") {
			t.Errorf("a never-close text names a close: %q", s)
		}
	}

	// A new job resets the sync fields with the lines and the result.
	release := make(chan struct{})
	if err := g.d.runJob(func(*log.Logger) (string, error) { <-release; return "verified", nil }); err != nil {
		t.Fatal(err)
	}
	m = g.until("the next job runs", func(m map[string]any) bool { return m["running"] == true })
	if m["runNow"] != nil || m["syncNote"] != nil || m["syncLine"] != nil || m["note"] != nil {
		t.Errorf("the next job kept the Run now's sync fields: %v", m)
	}
	close(release)
	g.done()

	// The starting note, and a late Phase from the earlier session or another slot.
	g.s.waitedFor = 3 * time.Minute
	g.start()
	g.at(presync.PhaseStarting)
	g.at(presync.PhaseWaiting)
	if n := str(g.activity()["syncNote"]); !strings.Contains(n, "(3 min so far, up to 20)") {
		t.Errorf("3 minutes in: %q", n)
	}
	firstCfg.Phase(presync.PhaseStarting, time.Now()) // the earlier run's session, late
	g.d.cap.setPhase(g.d.cap.slot+7, presync.PhaseCancelling, time.Now())
	if m := g.activity(); m["syncPhase"] != presync.PhaseWaiting {
		t.Errorf("a late or foreign Phase call changed the note: %v", m["syncPhase"])
	}
	g.s.release <- struct{}{}
	g.done()
	for goos, want := range map[string]string{"darwin": "Starting Thunderbird out of sight…",
		"windows": "Starting Thunderbird minimized to the taskbar… its window may appear for a moment."} {
		if n := runNowSyncNote(goos, presync.PhaseStarting, time.Now(), time.Now(), false); n != want {
			t.Errorf("%s starting note %q, want %q", goos, n, want)
		}
	}

	// A session that panics: no phase left, its line said, the backup run.
	g.s.panics = true
	g.start()
	m = g.done()
	g.s.panics = false
	if m["syncPhase"] != "" || !strings.HasPrefix(str(m["note"]), "Thunderbird was not synced: the sync failed unexpectedly") || m["error"] != "" {
		t.Errorf("a panicking session: %v", m)
	}

	// The reload re-attaches the Run now poll (structural: no JavaScript engine here), on
	// the page of the archive it runs on (TestARunNowIsShownOnlyOnItsOwnArchive).
	if !strings.Contains(dashboardJS, "if(d.runNow&&d.runNowHere&&rb){rb.disabled=true;pollBackup();}else start();") {
		t.Error("a reload while a Run now runs does not re-attach its poll")
	}
	if !strings.Contains(dashboardJS, "if(n&&d.syncNote){n.hidden=false;n.textContent=d.syncNote;}") ||
		!strings.Contains(dashboardJS, "n.textContent=d.note||") {
		t.Error("pollBackup does not show the server's note")
	}

	// The start not allowed on this platform (C-49): no session, the note says why, no
	// outcome recorded.
	off := newRunNowRig(t, "darwin", false)
	off.start()
	m = off.done()
	if off.s.calls() != 0 || !strings.HasPrefix(str(m["note"]), startNotAllowedLine+" ") || m["error"] != "" {
		t.Errorf("start not allowed: session calls %d, note %q", off.s.calls(), m["note"])
	}
	if lr, _, err := state.ReadLastRun(off.out); err != nil || lr.Presync != nil || lr.Status != state.RunOK {
		t.Errorf("start not allowed: the record %+v (%v); want ok with no presync", lr, err)
	}

	// A Lock of an encrypted archive, or a switch away from it, clears its Run now's
	// activity, and its later lines are not kept (C-15). Positive twin first: a plaintext
	// Run now's activity is kept (and so is another encrypted job's, such as a copy,
	// which TestTheCardMakesAnUnencryptedCopy pins).
	c := g.d.cap
	c.mu.Lock()
	c.lines, c.result, c.jobOut, c.encrypted, c.onCurrent, c.runNow, c.syncLine = []string{"a line"}, "a result", g.out, false, true, true, "a sync line"
	c.mu.Unlock()
	c.forget(g.out)
	if m := g.activity(); m["result"] != "a result" || len(logLines(m["log"])) != 1 {
		t.Fatalf("twin: a plaintext job's activity was cleared: %v", m)
	}
	// The Lock button (api/encryption/lock) on a Desktop with keys, its job slot holding
	// an encrypted archive's Run now that still runs.
	store := graphconfig.NewFileSecretStore(t.TempDir())
	ld := newDashboard(Config{Out: g.out, GOOS: "darwin", SettingsPath: g.d.cfg.SettingsPath, Store: store,
		Keys: newArchiveKeys(store, readerLockedNote), Lifecycle: g.life})
	lc := ld.cap
	lc.mu.Lock()
	lc.lines, lc.result, lc.jobOut, lc.encrypted, lc.onCurrent, lc.runNow, lc.running = []string{"a line"}, "a result", g.out, true, true, true, true
	lc.mu.Unlock()
	if r := callAction(t, ld, ld.lockAction, nil); r["ok"] != true {
		t.Fatalf("Lock: %v", r)
	}
	lc.appendLine("a later line")
	lrec := httptest.NewRecorder()
	ld.activity(lrec, httptest.NewRequest(http.MethodGet, "/api/activity", nil))
	m = map[string]any{}
	json.Unmarshal(lrec.Body.Bytes(), &m)
	lc.mu.Lock()
	lc.running = false
	lc.mu.Unlock()
	if m["result"] != "" || len(logLines(m["log"])) != 0 || m["syncNote"] != nil {
		t.Errorf("a Lock left the encrypted Run now's activity, or kept a later line: %v", m)
	}
	c.mu.Lock()
	c.encrypted = true
	c.mu.Unlock()
	c.mu.Lock()
	c.lines, c.result, c.jobOut, c.muted = []string{"a line"}, "a result", g.out, false
	c.mu.Unlock()
	SaveSettings(g.d.cfg.SettingsPath, Settings{Out: t.TempDir()}) // the current archive is now another one
	if m := g.activity(); m["result"] != "" || len(logLines(m["log"])) != 0 {
		t.Errorf("a switch away left the encrypted job's activity: %v", m)
	}
}

// covers: MA-906
// The first-attended-sync line beside Run now (design-presync-attended §4.6, C-32,
// C-83): for a sync-on job where Run now starts Thunderbird, until the first
// attended Run now on this computer has started it — the marker is written when its
// session reports `starting` — and gone after. Never where the start is not allowed.
func TestRunNowFirstSyncLine(t *testing.T) {
	g := newRunNowRig(t, "darwin", true)
	spec := schedule.Spec{Name: schedule.DefaultNameFor(g.out), Interval: schedule.Daily, At: "05:00",
		Exe: "/usr/local/bin/mailarchive", Args: []string{"-job", g.jp}, Out: g.out}
	if err := schedule.WriteDescriptor(g.out, schedule.DescribeSpec(spec)); err != nil {
		t.Fatal(err)
	}
	line := "New: Run now starts Thunderbird out of sight, even while you are using the computer, waits up to 20 minutes for its mail, and leaves it open for you to quit from the Dock when you are done."
	page, _ := getPage(t, DashboardHandler(g.d.cfg))
	if !shows(page, line) {
		t.Fatal("twin: the first-attended-sync line is not beside Run now")
	}
	r := presync.OverrideTablesForTest(nil, map[string]bool{"darwin": false})
	page, _ = getPage(t, DashboardHandler(g.d.cfg))
	r()
	if strings.Contains(page, `id="runNowFirstSync"`) {
		t.Error("the line shows where Run now does not start Thunderbird")
	}
	g.start()
	g.at(presync.PhaseStarting)
	g.s.release <- struct{}{}
	g.done()
	if _, err := os.Stat(filepath.Join(g.dir, "attended-sync-started")); err != nil {
		t.Fatalf("the marker was not written when the session reached S5: %v", err)
	}
	if page, _ = getPage(t, DashboardHandler(g.d.cfg)); strings.Contains(page, `id="runNowFirstSync"`) {
		t.Error("the line still shows after the first attended Run now started Thunderbird")
	}
	if w := runNowFirstSyncLine("windows"); w != "New: Run now starts Thunderbird minimized to the taskbar, even while you are using the computer, waits up to 20 minutes for its mail, and leaves it open for you to quit from the taskbar or the notification area when you are done." {
		t.Errorf("the Windows first-sync line: %q", w)
	}
}

// covers: MA-929
// "Stop waiting and back up now" on the Desktop (design-presync-attended §4.6, A16,
// owner ruling OQ-6): offered only while a Run now waits for Thunderbird; the route
// api/runnow-stopwait (through d.api: POST only, the guard, the CSRF token), naming
// the running Run now's slot id, ends that run's EndWait, the session ends its wait
// (wait-ended, Thunderbird left open) and the backup goes on and reads — the run is
// not cancelled. Positive twin first. Then each refusal changes nothing — the session
// keeps waiting: no Run now running, a stale id, the sync still starting, and a wait
// already ended. The button's script uses busy().
func TestStopWaitRoute(t *testing.T) {
	g := newRunNowRig(t, "darwin", true)
	g.s.waitedFor = 4 * time.Minute
	g.start()
	g.at(presync.PhaseWaiting)
	m := g.activity()
	if m["stopWaitable"] != true || str(m["run"]) == "" {
		t.Fatalf("twin: Stop waiting is not offered while it waits: %v", m)
	}
	run := str(m["run"])
	reply := callAction(t, g.d, g.d.stopWaitAction, map[string]any{"run": run})
	if reply["ok"] != true || str(reply["note"]) != "Backing up the mail that has arrived… Thunderbird was left open out of sight." {
		t.Fatalf("twin: the press: %v", reply)
	}
	again := callAction(t, g.d, g.d.stopWaitAction, map[string]any{"run": run})
	m = g.done()
	if !strings.HasPrefix(str(m["note"]), "Thunderbird sync: you stopped the wait after 4 min, so the backup read the mail that had arrived by then") ||
		!strings.Contains(str(m["note"]), "Export complete.") || g.lastRunStatus() != state.RunOK || m["error"] != "" {
		t.Errorf("after the press the backup did not read and finish: %v, record %s", m, g.lastRunStatus())
	}
	cfg, _, _ := g.s.last()
	if cfg.EndWait == nil || cfg.EndWait.Err() == nil {
		t.Error("the session's EndWait was not the one the press ended")
	}
	assure.Refused(t, http.StatusOK, str(again["error"]), assure.Code(http.StatusOK), assure.Names("The wait has already ended", "Nothing was changed"),
		assure.NoSideEffect(func() bool { return again["ok"] != true }))

	// Nothing running.
	none := callAction(t, g.d, g.d.stopWaitAction, map[string]any{"run": run})
	assure.Refused(t, http.StatusOK, str(none["error"]), assure.Code(http.StatusOK), assure.Names("No backup is waiting for Thunderbird", "Nothing was changed"),
		assure.NoSideEffect(func() bool { return none["ok"] != true }))

	// A stale id, while a new Run now waits: refused, and the session keeps waiting.
	g.start()
	g.at(presync.PhaseWaiting)
	stale := callAction(t, g.d, g.d.stopWaitAction, map[string]any{"run": run})
	m = g.activity()
	cfg, _, _ = g.s.last()
	assure.Refused(t, http.StatusOK, str(stale["error"]), assure.Code(http.StatusOK), assure.Names("No backup is waiting for Thunderbird", "Nothing was changed"),
		assure.NoSideEffect(func() bool {
			return stale["ok"] != true && cfg.EndWait.Err() == nil && m["running"] == true && m["stopWaitable"] == true
		}))
	g.s.release <- struct{}{}
	g.done()

	// The sync still starting: refused, nothing ended.
	g.s.holdStart = true
	g.start()
	g.at(presync.PhaseStarting)
	if m := g.activity(); m["stopWaitable"] == true || m["syncPhase"] != presync.PhaseStarting {
		t.Errorf("Stop waiting offered while Thunderbird starts: %v", m)
	}
	early := callAction(t, g.d, g.d.stopWaitAction, map[string]any{"run": str(g.activity()["run"])})
	cfg, _, _ = g.s.last()
	// Said as it is: Thunderbird is still starting, and Cancel is the way out until it
	// waits for mail — never "No backup is waiting for Thunderbird" while one is
	// (friction review, §14 gate 3).
	assure.Refused(t, http.StatusOK, str(early["error"]), assure.Code(http.StatusOK),
		assure.Names("Thunderbird is still starting", "once it is waiting for mail", "Nothing was changed"),
		assure.NoSideEffect(func() bool { return early["ok"] != true && cfg.EndWait.Err() == nil }))
	callAction(t, g.d, g.d.cancelJobAction, nil)
	g.at(presync.PhaseCancelling)
	g.s.release <- struct{}{}
	g.done()

	// The route is POST-only and guarded, like every Desktop action.
	rec := httptest.NewRecorder()
	g.d.api(g.d.stopWaitAction)(rec, httptest.NewRequest(http.MethodGet, "/api/runnow-stopwait", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("a GET: %d, Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/runnow-stopwait", strings.NewReader(`{"run":"1"}`))
	req.Host = "127.0.0.1:8097"
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	g.d.api(g.d.stopWaitAction)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("a post with no CSRF token: %d, want 403", rec.Code)
	}

	page, _ := getPage(t, DashboardHandler(g.d.cfg))
	if !strings.Contains(dashboardJS, `fetch('api/runnow-stopwait',{method:'POST'`) || !strings.Contains(dashboardJS, `body:JSON.stringify({run:runNowID})`) {
		t.Error("the page never posts its Run now's id to api/runnow-stopwait")
	}
	if !regexp.MustCompile(`swb\.addEventListener\('click',async\(\)=>\{\s*const n=\$\('runBackupNote'\),done=busy\(swb,'Stopping the wait…'`).MatchString(dashboardJS) {
		t.Error("the Stop waiting button does not go through busy()")
	}
	if !strings.Contains(page, `id="jobStopWait" hidden title="`+stopWaitTitle+`"`) {
		t.Error("the Stop waiting button is not hidden by default with its tooltip")
	}
}

// covers: MA-906
// A Run now is shown only on the page of the archive it runs on (gate-2 adversarial
// pass 1 over AS4, as4web-2): /api/activity says whether the Run now runs on the
// current archive, and only then carries its slot id, phase note, Cancel, Stop waiting
// and end note. Positive twin first: on its own archive's page a waiting Run now has
// all of them. After a switch to another archive, that page gets none — no note, no
// log, no Cancel, no Stop waiting — only a note that a Run now of another archive is
// running, then that it has ended, never "Backup finished."; switched back, its own
// archive's page has its end note again. The reload re-attaches the Run now's poll
// only on its own archive's page, and the page shows the other-archive note.
func TestARunNowIsShownOnlyOnItsOwnArchive(t *testing.T) {
	g := newRunNowRig(t, "darwin", true)
	g.start()
	g.at(presync.PhaseWaiting)
	m := g.activity()
	if str(m["run"]) == "" || str(m["syncNote"]) == "" || m["stopWaitable"] != true || m["cancellable"] != true || m["otherNote"] != nil {
		t.Fatalf("twin: a waiting Run now on its own archive's page: %v", m)
	}
	if m["runNowHere"] != true {
		t.Errorf("twin: the Run now's own archive's page is not told it runs here: %v", m["runNowHere"])
	}

	other := t.TempDir()
	SaveSettings(g.d.cfg.SettingsPath, Settings{Out: other}) // "Switch to this archive" on another one
	m = g.activity()
	if m["running"] != true || m["runNow"] != true {
		t.Fatalf("after the switch: %v", m)
	}
	for _, k := range []string{"run", "syncNote", "cancelTitle", "note", "syncLine"} {
		if _, ok := m[k]; ok {
			t.Errorf("another archive's page gets the Run now's %q: %v", k, m[k])
		}
	}
	if m["runNowHere"] != false || m["stopWaitable"] == true || m["cancellable"] != false || len(logLines(m["log"])) != 0 ||
		str(m["otherNote"]) != runNowElsewhereRunning {
		t.Errorf("another archive's page: runNowHere %v, stopWaitable %v, cancellable %v, log %v, otherNote %q",
			m["runNowHere"], m["stopWaitable"], m["cancellable"], m["log"], m["otherNote"])
	}

	g.s.release <- struct{}{}
	m = g.done()
	if _, ok := m["note"]; ok || str(m["result"]) != "" || str(m["error"]) != "" || str(m["otherNote"]) != runNowElsewhereEnded {
		t.Errorf("the ended Run now on another archive's page: note %q, result %q, error %q, otherNote %q",
			m["note"], m["result"], m["error"], m["otherNote"])
	}
	if strings.Contains(str(m["otherNote"]), "Backup finished.") {
		t.Errorf("another archive's page says the backup finished: %q", m["otherNote"])
	}

	SaveSettings(g.d.cfg.SettingsPath, Settings{Out: g.out}) // switched back
	if m = g.activity(); m["runNowHere"] != true || !strings.HasPrefix(str(m["note"]), "Thunderbird synced: its mail settled after 6 min.") {
		t.Errorf("back on its own archive, the end note: %v", m)
	}

	if !strings.Contains(dashboardJS, "if(d.runNow&&d.runNowHere&&rb){rb.disabled=true;pollBackup();}else start();") {
		t.Error("the reload re-attaches the Run now's poll on another archive's page")
	}
	if !strings.Contains(dashboardJS, "n.textContent=d.note||d.otherNote||") || !strings.Contains(dashboardJS, "if(d.otherNote&&res){res.hidden=false;res.textContent=d.otherNote;}") {
		t.Error("the page does not show the other-archive note")
	}
}

// covers: MA-915, MA-922
// The Desktop texts AS4 builds (design-presync-attended §7, C-23, C-24, C-49, C-50,
// C-52, C-61, C-69, C-84, OQ-2, OQ-6, OQ-7), never-close only: Run now's tooltip for
// macOS and Windows where the start is allowed, the start-off variant where it is
// not, no sync sentence on Linux or with the sync off; rendered on the card for a
// job under a non-default name. Every Windows text says the taskbar (and the
// notification area), never the Dock or "out of sight"; no text says Thunderbird is
// closed, offers "Keep Thunderbird open", promises a prompt about a message being
// written, or names a command. Cancel's sync variant says the whole backup is
// skipped. The Settings card's SyncNotice says a Run now does not wait only where
// it starts Thunderbird. The release notes claim Run now syncs on a Mac only when
// macOS's start entry is true (the line is written now; it ships with the flip).
func TestRunNowTexts(t *testing.T) {
	type text struct{ goos, s string }
	var all []text
	for _, goos := range []string{"darwin", "windows"} {
		for _, allowed := range []bool{true, false} {
			r := presync.OverrideTablesForTest(nil, map[string]bool{goos: allowed})
			title := runNowTitle(goos, true)
			r()
			if !allowed {
				if title != runNowStartOffTitle {
					t.Errorf("%s, start not allowed: tooltip %q", goos, title)
				}
				continue
			}
			for _, want := range []string{"even while you are using the computer", "(up to 20 minutes, often about 13)", "then leaves it open for you to quit from",
				"A Thunderbird that is already open is left alone.", "You can cancel while it waits; that skips the backup."} {
				if !strings.Contains(title, want) {
					t.Errorf("%s tooltip lacks %q: %q", goos, want, title)
				}
			}
			all = append(all, text{goos, title})
		}
		all = append(all, text{goos, cancelSyncTitle(goos, presync.PhaseWaiting)}, text{goos, cancelSyncTitle(goos, presync.PhaseStarting)},
			text{goos, runNowFirstSyncLine(goos)},
			text{goos, runNowSyncNote(goos, presync.PhaseStarting, time.Now(), time.Now(), false)},
			text{goos, runNowSyncNote(goos, presync.PhaseWaiting, time.Now(), time.Now(), false)},
			text{goos, runNowSyncNote(goos, presync.PhaseCancelling, time.Now(), time.Now(), false)},
			text{goos, runNowSyncNote(goos, "", time.Now(), time.Now(), true)},
			text{goos, runNowCancelWords(goos, "cancelled", string(presync.LeftOpenByRule), true)},
			text{goos, runNowCancelWords(goos, "cancelled", string(presync.LeftMayBeStarting), true)},
			text{goos, runNowCancelWords(goos, "cancelled", string(presync.LeftExited), true)})
		// A Cancel that met the Thunderbird this backup started already gone (gate-2 pass
		// 1 re-check, new item 2): it was started, so never "before Thunderbird was
		// started"; nothing is left open, so no quit advice.
		if s, want := runNowCancelWords(goos, "cancelled", string(presync.LeftExited), false),
			"Cancelled. Thunderbird, started by this backup, was no longer running. The archive is unchanged."; s != want {
			t.Errorf("%s: a cancel with our Thunderbird already gone: %q, want %q", goos, s, want)
		}
		if s := runNowCancelWords(goos, "cancelled", string(presync.LeftNone), false); s != "Cancelled before Thunderbird was started. The archive is unchanged." {
			t.Errorf("%s: twin, a cancel before the start: %q", goos, s)
		}
		if !strings.Contains(cancelSyncTitle(goos, presync.PhaseWaiting), "the whole backup is skipped") ||
			!strings.Contains(cancelSyncTitle(goos, presync.PhaseWaiting), "click Stop waiting and back up now instead") ||
			strings.Contains(cancelSyncTitle(goos, presync.PhaseStarting), "Stop waiting") {
			t.Errorf("%s: Cancel's sync variant: %q / %q", goos, cancelSyncTitle(goos, presync.PhaseWaiting), cancelSyncTitle(goos, presync.PhaseStarting))
		}
	}
	all = append(all, text{"", stopWaitTitle}, text{"", startNotAllowedLine}, text{"", runNowStartOffTitle})
	for _, x := range all {
		if x.goos == "windows" && (strings.Contains(x.s, "Dock") || strings.Contains(x.s, "out of sight")) {
			t.Errorf("a Windows text names the Dock or 'out of sight': %q", x.s)
		}
		if x.goos == "windows" && strings.Contains(x.s, "quit it from") && !strings.Contains(x.s, "the taskbar or the notification area") {
			t.Errorf("a Windows text does not name the taskbar and the notification area: %q", x.s)
		}
		if x.goos == "darwin" && strings.Contains(x.s, "taskbar") {
			t.Errorf("a macOS text names the taskbar: %q", x.s)
		}
		for _, forbid := range []string{"closes it", "was closed", "is closed", "Keep Thunderbird open", "on-exit", "asks before", "being written", "mailarchive ", "`"} {
			if strings.Contains(x.s, forbid) {
				t.Errorf("a never-close Desktop text says %q: %q", forbid, x.s)
			}
		}
	}
	if s := runNowSyncNote("windows", presync.PhaseStarting, time.Now(), time.Now(), false); !strings.Contains(s, "its window may appear for a moment") {
		t.Errorf("the Windows starting note does not say the window may appear: %q", s)
	}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		if runNowTitle(goos, false) != runNowPlainTitle {
			t.Errorf("%s, sync off: tooltip %q", goos, runNowTitle(goos, false))
		}
	}
	r := presync.OverrideTablesForTest(nil, map[string]bool{"linux": true})
	if runNowTitle("linux", true) != runNowPlainTitle {
		t.Error("Linux shows a sync sentence")
	}
	r()

	// Rendered: a sync-on job under a non-default name; the card's notice.
	g := newRunNowRig(t, "darwin", true)
	custom := filepath.Join(filepath.Dir(g.jp), "mailarchive-my-own-name.json")
	if err := os.Rename(g.jp, custom); err != nil {
		t.Fatal(err)
	}
	if !runNowJobSyncs(g.out) {
		t.Fatal("the job Run now would run, under a non-default name, is not read")
	}
	if err := os.Rename(custom, g.jp); err != nil {
		t.Fatal(err)
	}
	spec := schedule.Spec{Name: schedule.DefaultNameFor(g.out), Interval: schedule.Daily, At: "05:00",
		Exe: "/usr/local/bin/mailarchive", Args: []string{"-job", g.jp}, Out: g.out}
	if err := schedule.WriteDescriptor(g.out, schedule.DescribeSpec(spec)); err != nil {
		t.Fatal(err)
	}
	page, _ := getPage(t, DashboardHandler(g.d.cfg))
	if !shows(page, runNowTitle("darwin", true)) || strings.Contains(page, "it first opens Thunderbird, waits for its mail, then closes it") {
		t.Error("the card's Run now tooltip is not the AS4 macOS text")
	}
	if !shows(page, "a backup you start with Run now does not wait for that, and leaves Thunderbird open for you to quit.") {
		t.Error("the Settings card's notice does not say a Run now does not wait, where Run now starts Thunderbird")
	}
	r = presync.OverrideTablesForTest(nil, map[string]bool{"darwin": false})
	page, _ = getPage(t, DashboardHandler(g.d.cfg))
	r()
	if !shows(page, runNowStartOffTitle) || shows(page, "a backup you start with Run now does not wait for that") {
		t.Error("with the start not allowed: the card's tooltip or notice")
	}

}

// covers: MA-915
// The release notes and the real start table agree (design-presync-attended §7, C-32,
// C-52, owner ruling OQ-7: ship the Mac first): the AS4 line (written now, as
// releaseLineMac) is in packaging/release-notes.md exactly when macOS's
// AttendedStartAllowed entry is true, and no line says Windows' Run now syncs while
// its entry is false. So a release can never announce a Run now sync its build does
// not do, and the flip commit cannot forget the line.
func TestRunNowReleaseLineFollowsTheTable(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the package")
		}
		dir = parent
	}
	notes, err := os.ReadFile(filepath.Join(dir, "packaging", "release-notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	// The friction review (§14 gate 3) dropped the doubled "Run now now"; the checks
	// below match the settled words, so none of them is looser than before.
	if !strings.Contains(releaseLineMac, "On a Mac, Run now syncs Thunderbird first") || !strings.Contains(releaseLineMac, "On Windows, Run now does not start Thunderbird yet") ||
		strings.Contains(releaseLineMac, "Run now now") {
		t.Fatalf("twin: the AS4 release line: %q", releaseLineMac)
	}
	if got := strings.Contains(string(notes), releaseLineMac); got != presync.AttendedStartAllowed("darwin") {
		t.Errorf("the release notes carry the Mac line: %v; macOS's start entry: %v", got, presync.AttendedStartAllowed("darwin"))
	}
	if (strings.Contains(string(notes), "Run now syncs Thunderbird") || strings.Contains(string(notes), "Run now now syncs Thunderbird")) &&
		!presync.AttendedStartAllowed("darwin") {
		t.Error("the release notes say Run now syncs while no platform's start entry is true")
	}
	if (strings.Contains(string(notes), "On Windows, Run now syncs Thunderbird") || strings.Contains(string(notes), "On Windows, Run now now syncs Thunderbird")) != presync.AttendedStartAllowed("windows") {
		t.Error("the release notes and the Windows start entry disagree")
	}
}

// covers: MA-915
// What a person sees with the tables as shipped, no override (design-presync-attended
// §7, §12 AS4, §14 gate 4; C-49, C-84, OQ-7): macOS's start entry is true since
// 2026-10-31 (labs L-A1 and L-A9, docs/lab/presync-attended-start-darwin.md, and the
// wait fix MA-932), so a Mac's plaintext Run now tooltip is the AS4 start text word for
// word, the encrypted one starts with it, the Settings notice says a Run now does not
// wait, and the first-sync line is offered. Windows follows its own entry: while it is
// false (no MA-918 record), every Windows text is the start-off variant.
func TestRunNowShippedTexts(t *testing.T) {
	const mac = "Run this archive's backup now. Because Thunderbird sync is on, it first starts Thunderbird out of sight — even while you are using the computer — and waits for its mail to arrive (up to 20 minutes, often about 13), then leaves it open for you to quit from the Dock. A Thunderbird that is already open is left alone. You can cancel while it waits; that skips the backup."
	if !presync.AttendedStartAllowed("darwin") || !runNowSyncs("darwin") {
		t.Fatal("macOS's start entry, as shipped, is not on")
	}
	if got := runNowTitle("darwin", true); got != mac {
		t.Errorf("a Mac's Run now tooltip, sync on: %q", got)
	}
	if got := encRunNowTitle("darwin", true); !strings.HasPrefix(got, mac+" ") || !strings.HasSuffix(got, encLockSentence) {
		t.Errorf("a Mac's encrypted Run now tooltip, sync on: %q", got)
	}
	if !strings.Contains(presync.SyncNoticeFor("darwin"), "; a backup you start with Run now does not wait for that, and leaves Thunderbird open for you to quit.") {
		t.Errorf("a Mac's sync notice does not say a Run now does not wait: %q", presync.SyncNoticeFor("darwin"))
	}
	if !strings.HasPrefix(runNowFirstSyncLine("darwin"), "New: Run now starts Thunderbird out of sight") {
		t.Errorf("a Mac's first-sync line: %q", runNowFirstSyncLine("darwin"))
	}
	if presync.AttendedStartAllowed("windows") {
		if runNowTitle("windows", true) == runNowStartOffTitle || !strings.Contains(runNowTitle("windows", true), "minimized to the taskbar") {
			t.Errorf("Windows' start entry is on, but its tooltip: %q", runNowTitle("windows", true))
		}
		return
	}
	if runNowSyncs("windows") || runNowTitle("windows", true) != runNowStartOffTitle || encRunNowTitle("windows", true) != encRunNowStartOff ||
		presync.SyncNoticeFor("windows") != presync.SyncNotice {
		t.Errorf("Windows' start entry is off, but a text says Run now starts Thunderbird: %q / %q", runNowTitle("windows", true), encRunNowTitle("windows", true))
	}
}
