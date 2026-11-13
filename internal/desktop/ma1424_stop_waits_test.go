package desktop

import (
	"context"
	"errors"
	"log"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/state"
)

// holdCancellable puts a cancellable job in d's slot that, once its context ends, takes
// lag to finish and writes its record whole as the engine does (state.WriteLastRun:
// write-then-rename), "cancelled". It returns when the job holds the slot; finished
// closes when the job has returned.
func holdCancellable(t *testing.T, d *dashboard, out string, lag time.Duration) (finished chan struct{}) {
	t.Helper()
	finished = make(chan struct{})
	started := make(chan struct{})
	err := d.runCancellableJobOn(out, func(j keyedJob, l *log.Logger) (string, error) {
		defer close(finished)
		begun := time.Now()
		if werr := state.WriteLastRun(out, state.LastRun{Status: state.RunRunning, Started: begun}); werr != nil {
			t.Errorf("the running record: %v", werr)
		}
		close(started)
		<-j.ctx.Done()
		time.Sleep(lag) // the engine reaching its next safe point
		if werr := state.WriteLastRun(out, state.LastRun{Status: state.RunCancelled, Started: begun, Finished: time.Now(), Error: "interrupted"}); werr != nil {
			t.Errorf("the cancelled record: %v", werr)
		}
		return "", j.ctx.Err()
	})
	if err != nil {
		t.Fatalf("the job did not take the slot: %v", err)
	}
	<-started
	return finished
}

// covers: MA-1424, R52
// Every stop waits for this process's running work (design-menubar MB-2). Positive twin
// first: with nothing running the stop drains at once. With a cancellable job in the
// slot (fault injection: it takes 300 ms to reach its safe point after its context ends),
// the stop cancels it and Drained closes only after the job has returned, its record
// whole on disk as "cancelled" and its slot result the interrupted words. A job that is
// not cancellable (a check, a rebuild) is waited for until the bound: the drain then
// returns the bound's error and the slot says interrupted. The wizard's export in this
// process is waited for too (wizard.WaitExports is registered by the instance).
func TestMA1424_EveryStopWaitsForRunningWork(t *testing.T) {
	// Positive twin: idle.
	life0 := NewLifecycle(context.Background())
	newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Lifecycle: life0})
	start := time.Now()
	life0.Stop()
	select {
	case <-life0.Drained():
	case <-time.After(5 * time.Second):
		t.Fatalf("an idle stop did not drain")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("an idle stop took %v", time.Since(start))
	}

	// A cancellable job.
	life := NewLifecycle(context.Background())
	d := newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Lifecycle: life})
	out := t.TempDir()
	finished := holdCancellable(t, d, out, 300*time.Millisecond)
	life.Stop()
	select {
	case <-life.Drained():
	case <-time.After(10 * time.Second):
		t.Fatalf("the stop never drained")
	}
	select {
	case <-finished:
	default:
		t.Fatalf("Drained closed before the job in the slot had returned: the stop did not wait")
	}
	lr, st, err := state.ReadLastRun(out)
	if err != nil || st != state.LastRunPresent || lr.Status != state.RunCancelled {
		t.Errorf("the record after the stop: %+v (%v, %v), want a whole cancelled record", lr, st, err)
	}
	waitUntil(t, "the slot's result", func() bool { d.cap.mu.Lock(); defer d.cap.mu.Unlock(); return !d.cap.running })
	d.cap.mu.Lock()
	result := d.cap.result
	d.cap.mu.Unlock()
	if result != interruptedText {
		t.Errorf("the slot's result %q, want %q", result, interruptedText)
	}

	// A job that is not cancellable: waited for until the bound.
	life2 := NewLifecycle(context.Background())
	d2 := newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Lifecycle: life2})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	if err := d2.runJob(func(*log.Logger) (string, error) { <-release; return "checked", nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start = time.Now()
	if err := d2.drainSlot(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the drain of a job that does not end: %v, want the bound's error", err)
	}
	if el := time.Since(start); el < 250*time.Millisecond {
		t.Errorf("the drain gave up after %v, before its bound", el)
	}
	d2.cap.mu.Lock()
	lines := strings.Join(d2.cap.lines, "\n")
	d2.cap.mu.Unlock()
	if !strings.Contains(lines, interruptedText) {
		t.Errorf("the slot after the bound: %q, want the interrupted line", lines)
	}
}

// covers: MA-1424, MA-1422, R52, R39
// The end of the session asks nothing and waits (design-menubar MB-2 items 1 and 3).
// Through the instance's own event-loop seam (an operating-system seam: AppKit's
// terminate request, Windows' WM_ENDSESSION): with a backup held in the slot, Busy says
// so (Windows' shutdown-block reason), EndSession stops the instance without showing the
// Quit question, and returns only once the serving has drained — the job returned, its
// record whole — or the bound ran out.
func TestMA1424_TheEndOfTheSessionAsksNothingAndWaits(t *testing.T) {
	s := installFrontSeams(t)
	g := newDRig(t)
	env := g.env(g.rec)
	evc := make(chan LoopEvents, 1)
	env.Loop = func(serve func() error, ev LoopEvents) error { evc <- ev; return serve() }
	var dash atomic.Pointer[dashboard]
	instanceDashboardMade = func(x *dashboard) { dash.Store(x) }
	t.Cleanup(func() { instanceDashboardMade = nil })
	g.startInstance(env, "-background")
	ev := <-evc
	d := dash.Load()
	if d == nil {
		t.Fatalf("the instance's dashboard was not made")
	}
	out := t.TempDir()
	finished := holdCancellable(t, d, out, 300*time.Millisecond)
	if !ev.Busy() {
		t.Errorf("Busy is false while a job runs: Windows would not show the shutdown-block reason")
	}
	ended := make(chan struct{})
	go func() { ev.EndSession(10 * time.Second); close(ended) }()
	select {
	case <-ended:
	case <-time.After(20 * time.Second):
		t.Fatalf("EndSession did not return within its bound")
	}
	select {
	case <-finished:
	default:
		t.Errorf("EndSession returned before the job had returned")
	}
	if _, _, q, _ := s.snap(); len(q) != 0 {
		t.Errorf("the end of the session asked %q; it must ask nothing", q)
	}
	select {
	case <-g.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("the instance did not end")
	}
	g.cancel = nil
	if instance.LockIsHeld(g.dir, "") {
		t.Errorf("the lock is still held after the end of the session")
	}
	if lr, _, _ := state.ReadLastRun(out); lr.Status != state.RunCancelled {
		t.Errorf("the record after the end of the session: %q, want cancelled", lr.Status)
	}
}

// covers: MA-1422, MA-695, R52
// Shut down is the person's own stop (the fold's second contradiction): while work runs
// it asks first, with Keep running the default, and nothing stops; Quit (confirm=1) then
// stops and waits. Positive twin first: idle, Shut down stops at once with no question.
// The question names no archive (the fold's first contradiction).
func TestMA1422_ShutDownAsksWhileWorkRuns(t *testing.T) {
	post := func(d *dashboard, url string) string {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", url, nil)
		req.Header.Set("Origin", "http://127.0.0.1:8097")
		req.Header.Set("X-CSRF-Token", d.csrf)
		d.shutdown(w, req)
		return w.Body.String()
	}
	// Idle.
	life0 := NewLifecycle(context.Background())
	d0 := newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Lifecycle: life0})
	if body := post(d0, "http://127.0.0.1:8097/api/shutdown"); !strings.Contains(body, `"ok":true`) || strings.Contains(body, `"ask"`) {
		t.Fatalf("an idle Shut down: %s, want ok with no question", body)
	}
	select {
	case <-life0.Context().Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("an idle Shut down did not stop")
	}

	// Busy.
	life := NewLifecycle(context.Background())
	d := newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Lifecycle: life})
	archive := filepath.Join(t.TempDir(), "Work mail")
	finished := holdCancellable(t, d, t.TempDir(), 200*time.Millisecond)
	body := post(d, "http://127.0.0.1:8097/api/shutdown")
	if !strings.Contains(body, `"ask":true`) || !strings.Contains(body, "MailArchive is busy (a task is running). Quit anyway?") ||
		!strings.Contains(body, `"keep":"Keep running"`) || !strings.Contains(body, `"quit":"Quit"`) || strings.Contains(body, filepath.Base(archive)) {
		t.Fatalf("Shut down while a job runs: %s, want the question with Keep running and Quit", body)
	}
	if life.Context().Err() != nil {
		t.Fatalf("Shut down stopped before the person answered the question")
	}
	if body := post(d, "http://127.0.0.1:8097/api/shutdown?confirm=1"); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("Quit: %s", body)
	}
	select {
	case <-life.Drained():
	case <-time.After(10 * time.Second):
		t.Fatalf("Quit did not drain")
	}
	select {
	case <-finished:
	default:
		t.Errorf("Quit's drain ended before the job returned")
	}
}
