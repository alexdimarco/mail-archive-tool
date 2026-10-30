package presync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/lockfile"
)

// The scheduled-run fixes of design-presync-attended slice AS1a (§2.4 A5, A9, A10,
// A11, §2.6; conditions C-1, C-2, C-3, C-5, C-8, C-10, C-45, C-60, C-77): they
// reach scheduled runs today, so every test here drives the unattended session.

var as1aBase = time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC)

const (
	as1aApp     = "/Applications/Thunderbird.app/Contents/MacOS/thunderbird"
	as1aProfile = "/Users/x/profile.default"
)

// syncingFake scripts a scheduled run's happy path: the owner away throughout, a
// Thunderbird that appears, fetches, goes quiet and exits after the quit request.
func syncingFake() *fakeEnv {
	f := newFakeEnv()
	f.sm = true
	f.boot = 1000
	f.idle, f.idleOK = 1200, true
	f.lockOK = true
	f.appearIn, f.exitIn = 4*time.Second, 2*time.Second
	f.changeAt = as1aBase.Add(24 * time.Second)
	f.dgBase, f.dgChanged = 1, 2
	f.nowT = as1aBase
	f.child = proc{pid: 200, exe: as1aApp, start: as1aBase.Add(1 * time.Second), args: "thunderbird -profile " + as1aProfile}
	return f
}

func syncingDeps(e env, statePath string) deps {
	return deps{
		env:          e,
		cfg:          quietConfig{settle: 10 * time.Second, quiet: 30 * time.Second, maxWait: 600 * time.Second},
		app:          as1aApp,
		profile:      as1aProfile,
		watch:        []string{"/w"},
		statePath:    statePath,
		lockPath:     "/l",
		base:         record{Version: recordVersion, Profile: as1aProfile, App: as1aApp, ToolPID: 1, ToolStart: 1},
		awaitPoll:    2 * time.Second,
		awaitUp:      90 * time.Second,
		pollEvery:    10 * time.Second,
		quitGrace:    120 * time.Second,
		idleAway:     120 * time.Second,
		quitIdleAway: 600 * time.Second,
	}
}

func cancelledCtx() context.Context {
	c, cancel := context.WithCancel(context.Background())
	cancel()
	return c
}

func hasRecordState(rs []recordState, want recordState) bool {
	for _, r := range rs {
		if r == want {
			return true
		}
	}
	return false
}

// covers: MA-900
// A11 (C-10): a cancel that came before S5 starts nothing. The run's context and
// the Stop are both checked right before the write-ahead record is saved and again
// right before the start; either done ends the session `cancelled` with zero
// launch requests and no `launching` record left behind. Positive twin first: the
// same session on a live context launches once and syncs.
func TestPresyncCancelBeforeLaunchStartsNothing(t *testing.T) {
	f := syncingFake()
	if res := runSession(context.Background(), syncingDeps(f, filepath.Join(t.TempDir(), "s.json"))); res.outcome != OutcomeOK || f.startCalls != 1 {
		t.Fatalf("twin: outcome %q with %d launch requests, want ok and exactly one", res.outcome.Name, f.startCalls)
	}

	for _, c := range []struct {
		name      string
		ctx, sctx context.Context
	}{
		{"the run cancelled before the session", cancelledCtx(), nil},
		{"Stop ended before the session", context.Background(), cancelledCtx()},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := syncingFake()
			sp := filepath.Join(t.TempDir(), "s.json")
			d := syncingDeps(f, sp)
			d.sctx = c.sctx
			res := runSession(c.ctx, d)
			if res.outcome != OutcomeCancelled {
				t.Errorf("outcome = %q, want cancelled", res.outcome.Name)
			}
			if f.startCalls != 0 {
				t.Errorf("%d launch requests after a cancel, want 0", f.startCalls)
			}
			if hasRecordState(res.records, stateLaunching) {
				t.Errorf("a launching record was saved after a cancel: %v", res.records)
			}
			if _, err := os.Stat(sp); !os.IsNotExist(err) {
				t.Errorf("a state record was left behind: %v", err)
			}
		})
	}

	// The second check: a cancel that lands while the launching record is being
	// saved still stops the start, and the record just written is removed.
	t.Run("cancelled during the launching save", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		old := RefuseSettingsWrite
		t.Cleanup(func() { RefuseSettingsWrite = old })
		RefuseSettingsWrite = func(string, string) error { cancel(); return nil }

		f := syncingFake()
		sp := filepath.Join(t.TempDir(), "s.json")
		res := runSession(ctx, syncingDeps(f, sp))
		if res.outcome != OutcomeCancelled || f.startCalls != 0 {
			t.Errorf("outcome %q with %d launch requests, want cancelled and 0", res.outcome.Name, f.startCalls)
		}
		if _, err := os.Stat(sp); !os.IsNotExist(err) {
			t.Errorf("the launching record was left behind after a cancel: %v", err)
		}
	})
}

// covers: MA-900
// A9 (C-8): Thunderbird is launched only after the write-ahead `launching` record
// is on disk. A save that fails ends the session `cannot-start` (a warning) with
// zero launch requests, so a Thunderbird started with no record can never be
// mistaken next time for the person's own. Positive twin first: a writable state
// directory launches.
func TestPresyncLaunchOnlyAfterRecordSaved(t *testing.T) {
	f := syncingFake()
	if res := runSession(context.Background(), syncingDeps(f, filepath.Join(t.TempDir(), "s.json"))); f.startCalls != 1 || !hasRecordState(res.records, stateLaunching) {
		t.Fatalf("twin: %d launch requests, records %v; want one launch after a saved launching record", f.startCalls, res.records)
	}

	f = syncingFake()
	notADir := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	sp := filepath.Join(notADir, "s.json") // its folder is a file: the save cannot succeed
	res := runSession(context.Background(), syncingDeps(f, sp))
	if res.outcome != OutcomeCannotStart {
		t.Errorf("outcome = %q, want cannot-start", res.outcome.Name)
	}
	if f.startCalls != 0 {
		t.Errorf("%d launch requests with no write-ahead record saved, want 0", f.startCalls)
	}
	if msg, warn := WarnText(res.outcome); !warn || msg == "" {
		t.Errorf("cannot-start must surface as a warning, got warn=%v %q", warn, msg)
	}
}

// covers: MA-900
// A9: a `quitting` or `left-running` record that cannot be saved becomes the side
// warning presync_state_unsaved (once), so the owner learns a Thunderbird left open
// may go unannounced. Positive twin first: with the state folder writable, the same
// session has no side warning.
func TestPresyncUnsavedRecordIsASideWarning(t *testing.T) {
	f := syncingFake()
	f.quitErr = errQuitNotPermitted // ends left-running: saves quitting, then left-running
	res := runSession(context.Background(), syncingDeps(f, filepath.Join(t.TempDir(), "s.json")))
	if res.outcome != OutcomeQuitNotPermitted || len(res.side) != 0 {
		t.Fatalf("twin: outcome %q, side %v; want quit-not-permitted and no side warning", res.outcome.Name, res.side)
	}

	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	f = syncingFake()
	f.quitErr = errQuitNotPermitted
	var once sync.Once
	f.digestHook = func() { // in S7, the state folder becomes a file: later saves fail
		once.Do(func() {
			if err := os.RemoveAll(dir); err != nil {
				t.Error(err)
			}
			if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
				t.Error(err)
			}
		})
	}
	res = runSession(context.Background(), syncingDeps(f, filepath.Join(dir, "s.json")))
	if res.outcome != OutcomeQuitNotPermitted {
		t.Errorf("outcome = %q, want quit-not-permitted (the unsaved record never changes the outcome)", res.outcome.Name)
	}
	if len(res.side) != 1 || res.side[0] != OutcomeStateUnsaved {
		t.Errorf("side = %v, want exactly [state-unsaved]", res.side)
	}
	if got := res.result().Side; len(got) != 1 || got[0] != OutcomeStateUnsaved {
		t.Errorf("Result.Side = %v, want the side warning handed to the caller", got)
	}
}

// covers: MA-900
// sleepCtx (A5, C-2/T4) — untagged, so the Linux gate runs the very wait both
// adapters use: on a real clock it returns well before d when its context or its
// Stop ends, and reports that it was cut short. Positive twin first: with nothing
// ended it waits the whole d and says so.
func TestSleepCtxReturnsOnCancel(t *testing.T) {
	start := time.Now()
	if full := sleepCtx(context.Background(), nil, 30*time.Millisecond); !full || time.Since(start) < 30*time.Millisecond {
		t.Fatalf("twin: sleepCtx returned full=%v after %v, want the whole 30ms", full, time.Since(start))
	}

	for _, c := range []struct {
		name string
		run  func(ctx, stop context.Context) bool
		end  func(cancelCtx, cancelStop context.CancelFunc)
	}{
		{"the context ends", func(ctx, _ context.Context) bool { return sleepCtx(ctx, nil, 10*time.Second) }, func(c, _ context.CancelFunc) { c() }},
		{"the Stop ends", func(_, stop context.Context) bool { return sleepCtx(nil, stop, 10*time.Second) }, func(_, s context.CancelFunc) { s() }},
	} {
		ctx, cancelCtx := context.WithCancel(context.Background())
		stop, cancelStop := context.WithCancel(context.Background())
		time.AfterFunc(20*time.Millisecond, func() { c.end(cancelCtx, cancelStop) })
		start := time.Now()
		full := c.run(ctx, stop)
		took := time.Since(start)
		cancelCtx()
		cancelStop()
		if full {
			t.Errorf("%s: sleepCtx reported the whole wait elapsed", c.name)
		}
		if took > 2*time.Second {
			t.Errorf("%s: sleepCtx took %v of a 10s wait after a 20ms cancel", c.name, took)
		}
	}
}

// covers: MA-900
// The shared pre-quit function at S8 (A10, C-1): ownership re-verified, then the C3
// count from the RAW pid list, then `quitting`, then one request whose context has
// the 20 s deadline (C-5). Positive twin first: one Thunderbird, macOS-like
// bundle-wide quit → exactly one request on a live context, and ok. Then each
// guard sends ZERO requests: a second Thunderbird listed; one listed Thunderbird
// whose details cannot be read (SAF-9: it counts as a second one); the run
// cancelled or the Stop ended at S8 (before the function) and during the raw count
// (inside it, after its first check).
func TestPresyncPreQuitTwins(t *testing.T) {
	f := syncingFake()
	f.quitAll = true
	res := runSession(context.Background(), syncingDeps(f, filepath.Join(t.TempDir(), "s.json")))
	if res.outcome != OutcomeOK || f.quitCalls != 1 {
		t.Fatalf("twin: outcome %q with %d quit requests, want ok and exactly one", res.outcome.Name, f.quitCalls)
	}
	if f.quitCtxErr != nil {
		t.Errorf("twin: the quit context was already done at the call: %v", f.quitCtxErr)
	}
	if f.quitDeadline <= quitTimeout-2*time.Second || f.quitDeadline > quitTimeout {
		t.Errorf("twin: the quit context's deadline was %v away, want about %v (C-5)", f.quitDeadline, quitTimeout)
	}
	if f.sleeps == 0 || f.sleepsNoStop != 0 {
		t.Errorf("twin: %d of %d waits ran without the session context; every wait must watch it (A5)", f.sleepsNoStop, f.sleeps)
	}

	type setup func(f *fakeEnv, d *deps, cancelRun context.CancelFunc)
	stopAt := func(probe string, viaStop bool) setup {
		return func(f *fakeEnv, d *deps, cancelRun context.CancelFunc) {
			end := cancelRun
			if viaStop {
				stop, cancelStop := context.WithCancel(context.Background())
				d.sctx, end = stop, cancelStop
			}
			f.probeHook = func(name string) {
				if name == probe && f.started {
					end()
				}
			}
		}
	}
	for _, c := range []struct {
		name string
		set  setup
		want Outcome
	}{
		{"a second Thunderbird listed", func(f *fakeEnv, _ *deps, _ context.CancelFunc) {
			f.extraProcs = []proc{{pid: 201, exe: as1aApp, start: as1aBase.Add(2 * time.Second), args: "thunderbird -profile /Users/x/other.default"}}
		}, OutcomeQuitSkippedOthers},
		{"one listed Thunderbird unreadable", func(f *fakeEnv, _ *deps, _ context.CancelFunc) { f.unreadable = 1 }, OutcomeQuitSkippedOthers},
		{"the run cancelled at S8", stopAt("idle", false), OutcomeCancelled},
		{"the Stop ended at S8", stopAt("idle", true), OutcomeCancelled},
		{"the run cancelled during the raw count", stopAt("raw", false), OutcomeCancelled},
		{"the Stop ended during the raw count", stopAt("raw", true), OutcomeCancelled},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := syncingFake()
			f.quitAll = true
			sp := filepath.Join(t.TempDir(), "s.json")
			d := syncingDeps(f, sp)
			c.set(f, &d, cancel)
			res := runSession(ctx, d)
			if f.quitCalls != 0 {
				t.Errorf("%d quit requests, want 0", f.quitCalls)
			}
			if res.outcome != c.want {
				t.Errorf("outcome = %q, want %q", res.outcome.Name, c.want.Name)
			}
			if rec, present, _ := loadRecord(sp); !present || rec.State != stateLeftRunning || rec.ChildPID != 200 {
				t.Errorf("record present=%v state=%q pid=%d, want left-running with the child's identity", present, rec.State, rec.ChildPID)
			}
		})
	}
}

// covers: MA-900
// A quit that posted nothing (errQuitNotPosted, A10 step 7, C-60) ends AT ONCE as
// presync_quit_no_window: no exit-grace wait, the record left-running, and no text
// saying Thunderbird was asked to close (today a scheduled run waits out the
// 2-minute grace and then says it "did not close ... after it was asked to quit").
// Positive twin first: a posted quit waits for the exit and ends ok.
func TestPresyncQuitNotPostedEndsAtOnce(t *testing.T) {
	f := syncingFake()
	res := runSession(context.Background(), syncingDeps(f, filepath.Join(t.TempDir(), "s.json")))
	if res.outcome != OutcomeOK || f.sleeps == f.sleepsAtQuit {
		t.Fatalf("twin: outcome %q, %d sleeps after the request; want ok after an exit wait", res.outcome.Name, f.sleeps-f.sleepsAtQuit)
	}

	f = syncingFake()
	f.quitErr = errQuitNotPosted
	sp := filepath.Join(t.TempDir(), "s.json")
	res = runSession(context.Background(), syncingDeps(f, sp))
	if res.outcome != OutcomeQuitNoWindow {
		t.Errorf("outcome = %q, want quit-no-window", res.outcome.Name)
	}
	if f.quitCalls != 1 {
		t.Errorf("%d quit calls, want the one that posted nothing", f.quitCalls)
	}
	if f.sleeps != f.sleepsAtQuit {
		t.Errorf("the session waited %d polls after a quit that posted nothing, want none", f.sleeps-f.sleepsAtQuit)
	}
	if rec, present, _ := loadRecord(sp); !present || rec.State != stateLeftRunning {
		t.Errorf("record present=%v state=%q, want left-running", present, rec.State)
	}
	msg, warn := WarnText(OutcomeQuitNoWindow)
	if !warn {
		t.Error("quit-no-window must surface as a warning")
	}
	for _, text := range []string{msg, SummaryLine(OutcomeQuitNoWindow)} {
		if strings.Contains(text, "asked to") || strings.Contains(text, "being asked") {
			t.Errorf("quit-no-window text claims Thunderbird was asked to close: %q", text)
		}
	}
}

// covers: MA-901
// B6 (A6, C-45): an unreadable state record's warning reaches the caller while the
// session goes on to sync — the session ends ok and carries presync_state_unreadable
// as a side outcome (Result.Side), where before it was dropped. Twin: with no
// record, no side outcome. A proceeding NOTE-class recovery outcome (a record whose
// Thunderbird is not ours: already-running) adds no side outcome.
func TestPresyncUnreadableRecordWarnsWhileSyncing(t *testing.T) {
	f := syncingFake()
	res := runSession(context.Background(), syncingDeps(f, filepath.Join(t.TempDir(), "s.json")))
	if res.outcome != OutcomeOK || len(res.side) != 0 {
		t.Fatalf("twin: outcome %q, side %v; want ok and no side outcome", res.outcome.Name, res.side)
	}

	sp := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(sp, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	f = syncingFake()
	res = runSession(context.Background(), syncingDeps(f, sp))
	if res.outcome != OutcomeOK || f.startCalls != 1 {
		t.Errorf("outcome %q with %d launches, want the session to go on and sync (ok)", res.outcome.Name, f.startCalls)
	}
	if got := res.result().Side; len(got) != 1 || got[0] != OutcomeStateUnreadable {
		t.Errorf("Result.Side = %v, want [state-unreadable]", got)
	}

	// A note-class proceeding recovery outcome: a syncing record whose process is
	// not ours (matchOther → already-running, proceed). No side outcome.
	sp = filepath.Join(t.TempDir(), "s.json")
	if err := storeRecord(sp, record{Version: recordVersion, Boot: 1000, State: stateSyncing, App: as1aApp, ChildPID: 200, ChildStart: 1}); err != nil {
		t.Fatal(err)
	}
	f = newFakeEnv() // static: another Thunderbird is open
	f.boot = 1000
	f.idle, f.idleOK = 1200, true
	f.lockOK = true
	f.procs = []proc{{pid: 77, exe: as1aApp, start: as1aBase.Add(-time.Hour), args: "thunderbird"}}
	res = runSession(context.Background(), syncingDeps(f, sp))
	if res.outcome != OutcomeAlreadyRunning {
		t.Fatalf("setup: outcome %q, want already-running", res.outcome.Name)
	}
	if len(res.side) != 0 {
		t.Errorf("a note-class recovery outcome became a side warning: %v (C-45)", res.side)
	}
}

// covers: MA-921
// The computed budget (§2.6, C-2): the fake drives the LONGEST unattended path with
// every bound at its worst — the launcher takes launchTimeout and the quit request
// quitTimeout; a stale profile lock makes the running checks probe its pid;
// Thunderbird appears as late as it still can (every appear time is tried);
// nothing ever arrives, so S7 runs to the maximum wait; macOS's bundle-wide quit
// makes the raw count run; and Thunderbird never exits, so the exit wait runs to
// its grace. (Appearing earlier only shortens S6, so the appear times tried are the
// last 15 s of the await, more than one S6 pass, which covers every alignment of
// the last pass.) Each probe takes probeTimeout, or a little less (several probe times
// are tried: a probe that ends just under its bound lets each poll loop overrun its
// limit by one pass, which a probe of exactly 5 s hides). With resolve's two probes
// added, that path fits inside sessionBudget() with the slack still left over for
// the folder walk — so a constant changed later re-checks itself. It also fixes
// T21: today's 1440 s was shorter than the unattended worst path (about 1444 s
// before probes).
func TestSessionBudgetCoversLongestPath(t *testing.T) {
	if b := sessionBudget(); b <= 1444*time.Second {
		t.Errorf("sessionBudget() = %v, not above the unattended worst path before probes (T21)", b)
	}

	var longest, longestProbe time.Duration
	var longestOutcome Outcome
	for _, probe := range []time.Duration{probeTimeout, probeTimeout - 100*time.Millisecond, probeTimeout * 9 / 10, probeTimeout * 4 / 5, probeTimeout / 2, time.Second} {
		for appear := defAwaitUp - 15*time.Second; appear <= defAwaitUp; appear += time.Second {
			f := newFakeEnv()
			f.sm = true
			f.boot = 1000
			f.idle, f.idleOK = 3600, true
			f.lockOK = true
			f.lockOn[as1aProfile], f.lockP[as1aProfile] = true, 999 // a stale lock: its pid is probed too
			f.probeCost, f.launchCost, f.quitCost = probe, launchTimeout, quitTimeout
			if took, o := runLongestPath(t, f, appear); took > longest {
				longest, longestProbe, longestOutcome = took, probe, o
			}
		}
	}
	if longestOutcome != OutcomeQuitTimeout {
		t.Fatalf("the longest path driven ended %q, want quit-timeout (the full path to the exit grace)", longestOutcome.Name)
	}
	path := longest + 2*longestProbe // resolve's plutil on each candidate app, before the session
	t.Logf("longest path %v (probes of %v), budget %v with %v slack", path, longestProbe, sessionBudget(), budgetSlack)
	if room := sessionBudget() - budgetSlack; path > room {
		t.Errorf("the longest path takes %v, more than the budget %v leaves before its %v slack (%v): probesOnPath or a bound is undercounted",
			path, sessionBudget(), budgetSlack, room)
	}
}

// runLongestPath runs one session over f set for the longest path, with
// Thunderbird appearing appear after the launch, and returns its fake duration.
func runLongestPath(t *testing.T, f *fakeEnv, appear time.Duration) (time.Duration, Outcome) {
	t.Helper()
	f.quitAll = true
	f.appearIn = appear
	f.exitIn = 24 * time.Hour                 // never closes: the exit wait runs to its grace
	f.changeAt = as1aBase.Add(24 * time.Hour) // nothing arrives: S7 runs to the maximum wait
	f.nowT = as1aBase
	f.child = proc{pid: 200, exe: as1aApp, start: as1aBase.Add(time.Hour), args: "thunderbird -profile " + as1aProfile}

	d := syncingDeps(f, filepath.Join(t.TempDir(), "s.json"))
	d.cfg = quietConfig{settle: defSettle, quiet: defQuiet, maxWait: defMaxWait}
	d.awaitPoll, d.awaitUp, d.pollEvery, d.quitGrace = defAwaitPoll, defAwaitUp, defPollEvery, defQuitGrace
	d.idleAway, d.quitIdleAway = defIdleAway, defQuitIdleAway
	res := runSession(context.Background(), d)
	return f.nowT.Sub(as1aBase), res.outcome
}

// realLockEnv is the fake with the REAL per-profile lock (internal/lockfile), so a
// test sees whether a second session would get the lock.
type realLockEnv struct {
	*fakeEnv
	lock *lockfile.Lock
}

func (r *realLockEnv) acquireLock(path string) (bool, error) {
	l, err := lockfile.Acquire(path)
	if err != nil {
		if errors.Is(err, lockfile.ErrHeld) {
			return false, nil
		}
		return false, err
	}
	r.lock = l
	return true, nil
}

func (r *realLockEnv) releaseLock(path string) {
	if r.lock != nil {
		_ = r.lock.Release()
		r.lock = nil
	}
	r.fakeEnv.releaseLock(path)
}

// lockFree reports whether a new session would get the profile lock now.
func lockFree(t *testing.T, path string) bool {
	t.Helper()
	l, err := lockfile.Acquire(path)
	if err != nil {
		if errors.Is(err, lockfile.ErrHeld) {
			return false
		}
		t.Fatal(err)
	}
	_ = l.Release()
	return true
}

// covers: MA-921
// The abandoned rule and the once-both lock latch (§2.6, C-3, C-77), with the real
// profile lock. A session stuck at S8 past the watchdog's budget: the watchdog
// returns internal-error at the budget and marks the session abandoned; when the
// session is released and reaches the pre-quit function afterwards, it starts NO
// quit request and saves no `quitting` record. The lock stays held through the
// caller's read and is freed only once BOTH the abandoned session has returned and
// the caller's Release has run, in either order; the next session then gets the
// lock, not profile-busy.
func TestWatchdogAbandonedSessionStartsNoQuitAndLatchesTheLock(t *testing.T) {
	for _, callerFirst := range []bool{true, false} {
		name := "the session returns first"
		if callerFirst {
			name = "the caller releases first"
		}
		t.Run(name, func(t *testing.T) {
			lockPath := filepath.Join(t.TempDir(), "presync-x.lock")
			f := syncingFake()
			f.quitAll = true
			atS8, unblock := make(chan struct{}), make(chan struct{})
			var once sync.Once
			f.probeHook = func(name string) {
				if name == "idle" && f.started {
					once.Do(func() { close(atS8) })
					<-unblock
				}
			}
			released := make(chan struct{}, 4)
			f.onRelease = func() { released <- struct{}{} }
			d := syncingDeps(&realLockEnv{fakeEnv: f}, filepath.Join(t.TempDir(), "s.json"))
			d.lockPath = lockPath

			sessionDone := make(chan struct{})
			var sres sessionResult
			res := runWatchdog(context.Background(), nil, 300*time.Millisecond, func(sc sessionCtl) Result {
				defer close(sessionDone)
				d.sctx, d.abandoned = sc.ctx, sc.abandoned
				sres = runSession(context.Background(), d)
				return sres.result()
			})
			<-atS8
			if res.Outcome != OutcomeInternalError {
				t.Errorf("the watchdog returned %q at its budget, want internal-error", res.Outcome.Name)
			}
			if res.Release == nil {
				t.Fatal("the watchdog returned no Release: the abandoned session's lock would never be freed (T21)")
			}
			if lockFree(t, lockPath) {
				t.Error("the profile lock was free during the caller's read after a budget timeout (C-77)")
			}

			if callerFirst {
				res.Release()
				if lockFree(t, lockPath) {
					t.Error("the lock was freed by the caller alone while the abandoned session still ran")
				}
				close(unblock)
				<-sessionDone
			} else {
				close(unblock)
				<-sessionDone
				time.Sleep(100 * time.Millisecond) // let the watchdog's goroutine record the return
				if lockFree(t, lockPath) {
					t.Error("the lock was freed by the session's return alone, before the caller's read ended")
				}
				res.Release()
			}
			select {
			case <-released:
			case <-time.After(5 * time.Second):
				t.Fatal("the lock was never released once both halves were done")
			}
			if !lockFree(t, lockPath) {
				t.Error("the lock is still held after both the session returned and Release ran")
			}
			res.Release() // idempotent

			if f.quitCalls != 0 {
				t.Errorf("the abandoned session started %d quit requests after the watchdog returned, want 0", f.quitCalls)
			}
			if hasRecordState(sres.records, stateQuitting) {
				t.Errorf("the abandoned session saved a quitting record: %v", sres.records)
			}

			// The next session gets the lock (owner active: it ends right after S2's lock).
			g := syncingFake()
			g.idle = 10
			nd := syncingDeps(&realLockEnv{fakeEnv: g}, filepath.Join(t.TempDir(), "s.json"))
			nd.lockPath = lockPath
			next := runSession(context.Background(), nd)
			if next.outcome == OutcomeProfileBusy {
				t.Error("the next session found the profile busy after the latch released the lock")
			}
			if next.release != nil {
				next.release()
			}
		})
	}
}

// covers: MA-921
// The watchdog returns AT ONCE when the Stop ends or the run is cancelled (§2.6
// (2), C-3, C-5), well before its budget, with `cancelled`; it marks the session
// abandoned and cancels the session context, so a session waiting on that context
// ends too — and starts no quit request.
func TestWatchdogReturnsOnStopAndCancel(t *testing.T) {
	for _, viaStop := range []bool{true, false} {
		name := "the run cancelled"
		if viaStop {
			name = "the Stop ended"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancelCtx := context.WithCancel(context.Background())
			defer cancelCtx()
			stop, cancelStop := context.WithCancel(context.Background())
			defer cancelStop()
			f := syncingFake()
			blocked, sessionDone, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var res Result
			var ctl sessionCtl
			go func() {
				defer close(returned)
				res = runWatchdog(ctx, stop, 3*time.Second, func(sc sessionCtl) Result {
					defer close(sessionDone)
					ctl = sc
					f.probeHook = func(name string) {
						if name == "idle" && f.started {
							close(blocked)
							<-sc.ctx.Done() // a wait on the session context
						}
					}
					d := syncingDeps(f, filepath.Join(t.TempDir(), "s.json"))
					d.sctx, d.abandoned = sc.ctx, sc.abandoned
					return runSession(ctx, d).result()
				})
			}()
			<-blocked
			start := time.Now()
			if viaStop {
				cancelStop()
			} else {
				cancelCtx()
			}
			select {
			case <-returned:
			case <-time.After(10 * time.Second):
				t.Fatal("the watchdog did not return")
			}
			if took := time.Since(start); took > time.Second {
				t.Errorf("the watchdog took %v to return, want at once (not its 3s budget)", took)
			}
			if res.Outcome != OutcomeCancelled {
				t.Errorf("outcome = %q, want cancelled", res.Outcome.Name)
			}
			select {
			case <-sessionDone:
			case <-time.After(5 * time.Second):
				t.Fatal("the session waiting on its context never ended: the watchdog did not cancel it")
			}
			if !ctl.isAbandoned() {
				t.Error("the watchdog returned without marking the session abandoned")
			}
			if f.quitCalls != 0 {
				t.Errorf("%d quit requests after a Stop or cancel, want 0", f.quitCalls)
			}
		})
	}
}

// covers: MA-921
// A quit request in flight when the watchdog gives up ends with the session
// context (C-3, C-5): the request's context derives from it, so it is cancelled
// at the budget rather than running on to its own 20 s deadline. The budget is
// 2 s, not less: on a loaded machine a shorter one could end before the fake
// session reaches its quit, and an abandoned session rightly sends none.
func TestWatchdogEndsAnInFlightQuit(t *testing.T) {
	f := syncingFake()
	f.quitBlocks = true // the request waits on its context, like osascript held by a prompt
	sessionDone := make(chan struct{})
	res := runWatchdog(context.Background(), nil, 2*time.Second, func(sc sessionCtl) Result {
		defer close(sessionDone)
		d := syncingDeps(f, filepath.Join(t.TempDir(), "s.json"))
		d.sctx, d.abandoned = sc.ctx, sc.abandoned
		return runSession(context.Background(), d).result()
	})
	if res.Outcome != OutcomeInternalError {
		t.Errorf("outcome = %q, want internal-error at the budget", res.Outcome.Name)
	}
	select {
	case <-sessionDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the quit request in flight did not end with the abandoned session's context")
	}
	if f.quitCalls != 1 || !errors.Is(f.quitEndErr, context.Canceled) {
		t.Errorf("quit calls %d, its context ended with %v; want one request cancelled by the watchdog", f.quitCalls, f.quitEndErr)
	}
}

// covers: MA-928
// The Windows launch's working folder is Thunderbird's own install folder (C-65),
// so a Thunderbird left open for hours never pins the folder Mail Archive was
// started in (an archive, a removable drive). winInstallDir is the untagged pure
// function buildLaunchCmd uses, so the Linux gate checks it on Windows paths.
func TestWinInstallDir(t *testing.T) {
	for _, c := range []struct{ exe, want string }{
		{`C:\Program Files\Mozilla Thunderbird\thunderbird.exe`, `C:\Program Files\Mozilla Thunderbird`},
		{`C:\Users\First Last\AppData\Local\Mozilla Thunderbird\thunderbird.exe`, `C:\Users\First Last\AppData\Local\Mozilla Thunderbird`},
		{`D:/Apps/Thunderbird/thunderbird.exe`, `D:/Apps/Thunderbird`},
		{`C:\thunderbird.exe`, `C:\`},
		{`\\server\share\Thunderbird\thunderbird.exe`, `\\server\share\Thunderbird`},
		{`thunderbird.exe`, ``},
	} {
		if got := winInstallDir(c.exe); got != c.want {
			t.Errorf("winInstallDir(%q) = %q, want %q", c.exe, got, c.want)
		}
	}
}
