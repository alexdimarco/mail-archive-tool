package presync

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The attended mode in the session (design-presync-attended slice AS1b, §2.1-§2.4
// A1, A2, A4(ii), A7, A8, A12, A13, A15, A16, §2.6, §2.7, §2.8; conditions C-18,
// C-43, C-49, C-50, C-51, C-68, C-72, C-74, C-79; owner rulings OQ-1, OQ-5, OQ-6,
// OQ-7). No caller sets Attended yet (AS3, AS4), so every test drives the session
// directly.

// attendedFake is AS1a's syncing fake with the owner AT the computer (idle 0 s):
// a scheduled session would skip, an attended one must not.
func attendedFake() *fakeEnv {
	f := syncingFake()
	f.idle, f.idleOK = 0, true
	return f
}

// attendedDeps are AS1a's deps for an attended run on a platform whose attended
// close is not allowed (every platform until slice AS-Q): never-close.
func attendedDeps(e env, statePath string) deps {
	d := syncingDeps(e, statePath)
	d.attended, d.neverQuit = true, true
	return d
}

// loseOwnershipAfterAdoption makes the child unprovable once S7 has begun: a
// second candidate with the same executable, profile and start time appears, so
// no single process is provably ours at the next ownership re-check.
func loseOwnershipAfterAdoption(f *fakeEnv) {
	f.digestHook = func() {
		twin := f.child
		twin.pid = 201
		f.extraProcs = []proc{twin}
	}
}

// exitBeforeTheRecheck makes the child exit on its own just before the session's
// first process listing after the one that adopted it — the ownership re-check of
// A12, A16 or a Cancel (S7 lists no processes).
func exitBeforeTheRecheck(f *fakeEnv) {
	adopted := false
	f.probeHook = func(name string) {
		if name != "procs" || !f.started || f.nowT.Before(f.startedAt.Add(f.appearIn)) {
			return
		}
		if !adopted {
			adopted = true
			return
		}
		f.quitted, f.quitAt, f.exitIn = true, f.nowT, 0
	}
}

// recordGone reports whether the state record file is absent.
func recordGone(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

// phaseLog records every Phase call a session makes.
type phaseLog struct {
	mu    sync.Mutex
	names []string
	since []time.Time
}

func (p *phaseLog) fn(name string, since time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.names = append(p.names, name)
	p.since = append(p.since, since)
}

func (p *phaseLog) got() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.names...)
}

// covers: MA-895
// A1: an attended session does not read the S4 owner-activity gate at all. Positive
// twin first: a scheduled session with the owner at the computer (idle 0 s) reads
// the idle clock and ends owner-active-skipped with zero launches. Attended, the
// same owner — and an idle reading that cannot be read at all — gives a launch and
// idleSeconds is never called.
func TestAttendedSkipsTheStartIdleGate(t *testing.T) {
	f := attendedFake()
	res := runSession(context.Background(), syncingDeps(f, filepath.Join(t.TempDir(), "s.json")))
	if res.outcome != OutcomeOwnerActiveSkipped || f.startCalls != 0 || f.idleCalls == 0 {
		t.Fatalf("twin: scheduled with the owner here: outcome %q, %d launches, %d idle reads; want owner-active-skipped, 0, >0",
			res.outcome.Name, f.startCalls, f.idleCalls)
	}

	for _, c := range []struct {
		name string
		idle float64
		ok   bool
	}{
		{"owner idle 0 s", 0, true},
		{"idle reading unreadable", 999, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := attendedFake()
			f.idle, f.idleOK = c.idle, c.ok
			res := runSession(context.Background(), attendedDeps(f, filepath.Join(t.TempDir(), "s.json")))
			if f.startCalls != 1 {
				t.Errorf("%d launch requests, want 1 (attended skips the S4 idle gate)", f.startCalls)
			}
			if f.idleCalls != 0 {
				t.Errorf("idleSeconds was read %d times in an attended session, want never", f.idleCalls)
			}
			if res.outcome != OutcomeOK {
				t.Errorf("outcome = %q, want ok", res.outcome.Name)
			}
		})
	}
}

// covers: MA-895, MA-929
// buildDeps carries the attended fields from the Config (§2.1): Attended, and only
// with it NeverQuit and EndWait (both "ignored when !Attended"), plus Phase and the
// Log the progress line goes to. A scheduled Config with NeverQuit or EndWait set
// still gets neither, so no scheduled run can stop a wait early or be told not to
// close (R38).
func TestBuildDepsCarriesTheAttendedFields(t *testing.T) {
	endWait, cancel := context.WithCancel(context.Background())
	defer cancel()
	var log bytes.Buffer
	var pl phaseLog
	base := Config{StateDir: filepath.Join(t.TempDir(), "state"), Log: &log, Phase: pl.fn, NeverQuit: true, EndWait: endWait}

	att := base
	att.Attended = true
	d := buildDeps(newFakeEnv(), "/a/thunderbird", t.TempDir(), att)
	if !d.attended || !d.neverQuit || d.endWait != endWait || d.phase == nil || d.log != &log {
		t.Errorf("attended deps = {attended:%v neverQuit:%v endWait set:%v phase set:%v log set:%v}, want all carried",
			d.attended, d.neverQuit, d.endWait != nil, d.phase != nil, d.log == &log)
	}

	d = buildDeps(newFakeEnv(), "/a/thunderbird", t.TempDir(), base)
	if d.attended || d.neverQuit || d.endWait != nil {
		t.Errorf("scheduled deps = {attended:%v neverQuit:%v endWait set:%v}, want none (ignored when !Attended)",
			d.attended, d.neverQuit, d.endWait != nil)
	}
}

// covers: MA-896
// Attended keeps every other check (§2.3): each case ends with the same outcome
// whether scheduled (owner away) or attended (owner here), and the attended run
// asks nothing to quit. Thunderbird already running by the profile lock (A) or by
// a listed process (B), open with another profile, one opened between S4 and S5
// (C5), the profile re-check failing, the profile lock busy, a leftover of an
// earlier backup, Thunderbird never appearing (await-up) and nothing arriving
// (the maximum wait).
func TestAttendedKeepsEveryOtherCheck(t *testing.T) {
	other := proc{pid: 9, exe: as1aApp, start: as1aBase.Add(-time.Hour), args: "thunderbird -profile /Users/x/other.default"}
	mine := proc{pid: 9, exe: as1aApp, start: as1aBase.Add(-time.Hour), args: "thunderbird -profile " + as1aProfile}
	static := func(f *fakeEnv) { f.sm = false }
	for _, c := range []struct {
		name     string
		set      func(f *fakeEnv, statePath string)
		want     Outcome
		launches int
	}{
		{"already running: the profile lock is held (A)", func(f *fakeEnv, _ string) {
			static(f)
			f.lockOn[as1aProfile], f.lockP[as1aProfile] = true, 0
		}, OutcomeAlreadyRunning, 0},
		{"already running: a Thunderbird is listed (B)", func(f *fakeEnv, _ string) {
			static(f)
			f.procs = []proc{mine}
		}, OutcomeAlreadyRunning, 0},
		{"open with another profile", func(f *fakeEnv, _ string) {
			static(f)
			f.procs = []proc{other}
		}, OutcomeAlreadyRunning, 0},
		{"one opened between S4 and S5 (C5)", func(f *fakeEnv, _ string) {
			static(f)
			n := 0
			f.probeHook = func(name string) {
				if name == "procs" {
					if n++; n == 3 { // S3's match, S4's check, then S5's re-check
						f.procs = []proc{other}
					}
				}
			}
		}, OutcomeAlreadyRunning, 0},
		{"the profile re-check fails", func(f *fakeEnv, _ string) { f.recheckFail = true }, OutcomeProfileUnusable, 0},
		{"the profile lock is busy", func(f *fakeEnv, _ string) { f.lockOK = false }, OutcomeProfileBusy, 0},
		{"a leftover of an earlier backup is open", func(f *fakeEnv, sp string) {
			static(f)
			left := proc{pid: 300, exe: as1aApp, start: as1aBase.Add(-time.Hour), args: "thunderbird -profile " + as1aProfile}
			f.procs = []proc{left}
			if err := storeRecord(sp, record{Version: recordVersion, Boot: 1000, State: stateLeftRunning, Profile: as1aProfile,
				App: as1aApp, ChildPID: 300, ChildStart: left.start.Unix()}); err != nil {
				t.Fatal(err)
			}
		}, OutcomeLeftoverFound, 0},
		{"Thunderbird never appears (await-up)", func(f *fakeEnv, _ string) { f.appearIn = 24 * time.Hour }, OutcomeDidNotStart, 1},
		{"nothing arrives (the maximum wait)", func(f *fakeEnv, _ string) { f.changeAt = as1aBase.Add(24 * time.Hour) }, OutcomeNoActivity, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, attended := range []bool{false, true} {
				f := syncingFake()
				if attended {
					f.idle = 0
				}
				sp := filepath.Join(t.TempDir(), "s.json")
				c.set(f, sp)
				d := syncingDeps(f, sp)
				if attended {
					d = attendedDeps(f, sp)
				}
				res := runSession(context.Background(), d)
				if res.outcome != c.want {
					t.Errorf("attended=%v: outcome = %q, want %q", attended, res.outcome.Name, c.want.Name)
				}
				if f.startCalls != c.launches {
					t.Errorf("attended=%v: %d launch requests, want %d", attended, f.startCalls, c.launches)
				}
				if attended && f.quitCalls != 0 {
					t.Errorf("attended: %d quit requests, want 0", f.quitCalls)
				}
			}
		})
	}
}

// covers: MA-896
// C-18 (OD-4): an attended run's job-named profile must also be listed in
// profiles.ini — the named profile no longer bypasses membership when attended.
// profileNamedFor is the untagged rule both adapters' runWith use (the darwin
// resolve test checks it end to end).
func TestAttendedNamedProfileIsNotTrustedByName(t *testing.T) {
	for _, c := range []struct {
		cfg  Config
		want bool
	}{
		{Config{ProfileNamed: true}, true},
		{Config{ProfileNamed: false}, false},
		{Config{ProfileNamed: true, Attended: true}, false},
		{Config{ProfileNamed: false, Attended: true}, false},
	} {
		if got := profileNamedFor(c.cfg); got != c.want {
			t.Errorf("profileNamedFor(named=%v attended=%v) = %v, want %v", c.cfg.ProfileNamed, c.cfg.Attended, got, c.want)
		}
	}
}

// covers: MA-897
// A2: attended skips the S8 owner-activity re-check. Positive twin first: a
// scheduled session whose owner came back before S8 is left open, owner-active,
// with zero quit requests. Attended with NeverQuit goes to A12 (zero requests, the
// S7 verdict as the outcome, left open by rule, the record cleared); attended with
// NeverQuit false (set directly in the deps; no caller can until AS-Q) keeps the
// shared pre-quit function: one request on a live context with the 20 s deadline,
// then closed — and that function still refuses when ownership is lost (a second
// candidate makes the child ambiguous) or a second Thunderbird is open on a
// bundle-wide quit. Waited is measured from "up" to the quit or the verdict, and
// Started is the launch.
func TestAttendedSkipsTheQuitIdleGate(t *testing.T) {
	f := syncingFake()
	f.idleReturnsAt, f.idleReturned = as1aBase.Add(40*time.Second), 0
	res := runSession(context.Background(), syncingDeps(f, filepath.Join(t.TempDir(), "s.json")))
	if res.outcome != OutcomeOwnerActive || f.quitCalls != 0 {
		t.Fatalf("twin: scheduled, owner back at S8: outcome %q, %d quits; want owner-active and 0", res.outcome.Name, f.quitCalls)
	}

	t.Run("never-close (A12)", func(t *testing.T) {
		f := attendedFake()
		sp := filepath.Join(t.TempDir(), "s.json")
		res := runSession(context.Background(), attendedDeps(f, sp))
		if res.outcome != OutcomeOK || res.verdict != OutcomeOK || f.quitCalls != 0 || f.idleCalls != 0 {
			t.Errorf("outcome %q verdict %q, %d quits, %d idle reads; want ok, ok, 0, 0", res.outcome.Name, res.verdict.Name, f.quitCalls, f.idleCalls)
		}
		if res.left != LeftOpenByRule || !recordGone(t, sp) {
			t.Errorf("Left = %q, record gone %v; want left-open-by-rule and the record cleared", res.left, recordGone(t, sp))
		}
		if !res.started.Equal(f.startedAt) {
			t.Errorf("Started = %v, want the launch %v", res.started, f.startedAt)
		}
		if res.waited < 30*time.Second || res.started.Add(res.waited).After(f.nowT) {
			t.Errorf("Waited = %v, want at least the 30 s quiet and within the session", res.waited)
		}
	})

	t.Run("NeverQuit false keeps the pre-quit function", func(t *testing.T) {
		f := attendedFake()
		sp := filepath.Join(t.TempDir(), "s.json")
		d := attendedDeps(f, sp)
		d.neverQuit = false
		res := runSession(context.Background(), d)
		if res.outcome != OutcomeOK || f.quitCalls != 1 || f.idleCalls != 0 {
			t.Fatalf("outcome %q, %d quits, %d idle reads; want ok, 1, 0", res.outcome.Name, f.quitCalls, f.idleCalls)
		}
		if f.quitCtxErr != nil || f.quitDeadline <= quitTimeout-2*time.Second || f.quitDeadline > quitTimeout {
			t.Errorf("the quit context: err %v, deadline %v away; want live with about %v (C-5, C-6)", f.quitCtxErr, f.quitDeadline, quitTimeout)
		}
		if res.left != LeftClosed || !recordGone(t, sp) {
			t.Errorf("Left = %q, record gone %v; want closed and cleared", res.left, recordGone(t, sp))
		}
		if res.started.Add(res.waited).After(f.quitAt) {
			t.Errorf("Waited = %v runs past the quit request", res.waited)
		}
	})

	t.Run("NeverQuit false: ownership lost at the quit", func(t *testing.T) {
		f := attendedFake()
		loseOwnershipAfterAdoption(f)
		d := attendedDeps(f, filepath.Join(t.TempDir(), "s.json"))
		d.neverQuit = false
		res := runSession(context.Background(), d)
		if f.quitCalls != 0 || res.outcome != OutcomeIdentityUnavailable {
			t.Errorf("outcome %q with %d quits, want identity-unavailable and 0 (a pid not provably ours is never asked)", res.outcome.Name, f.quitCalls)
		}
	})

	t.Run("NeverQuit false: a second Thunderbird on a bundle-wide quit", func(t *testing.T) {
		f := attendedFake()
		f.quitAll = true
		f.unreadable = 1
		d := attendedDeps(f, filepath.Join(t.TempDir(), "s.json"))
		d.neverQuit = false
		res := runSession(context.Background(), d)
		if f.quitCalls != 0 || res.outcome != OutcomeQuitSkippedOthers {
			t.Errorf("outcome %q with %d quits, want quit-skipped-others and 0", res.outcome.Name, f.quitCalls)
		}
	})
}

var progressRE = regexp.MustCompile(`^Thunderbird sync: still waiting for mail to arrive \((\d+) min so far, up to 20\)$`)

// progressMinutes returns the N of every progress line written to log, in order.
func progressMinutes(t *testing.T, log string) []int {
	t.Helper()
	var ns []int
	for _, line := range strings.Split(log, "\n") {
		if m := progressRE.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			ns = append(ns, n)
		} else if strings.Contains(line, "still waiting") {
			t.Errorf("a malformed progress line: %q", line)
		}
	}
	return ns
}

// covers: MA-897
// A7 (C-43): while it waits for mail the session writes, at most once a fake
// minute, "Thunderbird sync: still waiting for mail to arrive (N min so far, up to
// 20)", N in whole minutes from the start of the wait and never above 20, in both
// modes. With a 10 s poll and nothing arriving, N runs 1..19 once each (the 20th
// minute is the verdict). With a coarse 150 s poll, N still only goes up, never
// past the time waited.
func TestProgressLineOncePerMinute(t *testing.T) {
	for _, attended := range []bool{false, true} {
		for _, poll := range []time.Duration{10 * time.Second, 150 * time.Second} {
			f := syncingFake()
			f.changeAt = as1aBase.Add(24 * time.Hour) // nothing arrives
			d := syncingDeps(f, filepath.Join(t.TempDir(), "s.json"))
			if attended {
				f.idle = 0
				d = attendedDeps(f, d.statePath)
			}
			d.cfg = quietConfig{settle: defSettle, quiet: defQuiet, maxWait: defMaxWait}
			d.pollEvery = poll
			var log bytes.Buffer
			d.log = &log
			runSession(context.Background(), d)
			ns := progressMinutes(t, log.String())
			if poll == 10*time.Second {
				if len(ns) != 19 {
					t.Errorf("attended=%v poll %v: %d progress lines %v, want 19 (1..19)", attended, poll, len(ns), ns)
				}
				for i, n := range ns {
					if n != i+1 {
						t.Errorf("attended=%v: progress line %d says %d min, want %d", attended, i, n, i+1)
					}
				}
			} else if len(ns) == 0 {
				t.Errorf("attended=%v poll %v: no progress line in a 20-minute wait", attended, poll)
			}
			for i, n := range ns {
				if n < 1 || n > 20 || (i > 0 && n <= ns[i-1]) {
					t.Errorf("attended=%v poll %v: progress minutes %v are not increasing within 1..20", attended, poll, ns)
					break
				}
			}
		}
	}
}

// covers: MA-897
// The end line of runWith (§4.6): an attended run is told with OutcomeLine (it
// knows whether Thunderbird was closed); a scheduled run keeps SummaryLine, the
// live log's line (§7).
func TestEndLineByMode(t *testing.T) {
	r := sessionResult{outcome: OutcomeOK, left: LeftOpenByRule, waited: 6 * time.Minute}
	if got, want := endLine(Config{Attended: true, Surface: SurfaceDesktop, GOOS: "darwin"}, r),
		OutcomeLine(OutcomeOK, SurfaceDesktop, "darwin", LeftOpenByRule, 6*time.Minute, false); got != want {
		t.Errorf("attended end line = %q, want OutcomeLine's %q", got, want)
	}
	if got, want := endLine(Config{}, r), SummaryLine(OutcomeOK); got != want {
		t.Errorf("scheduled end line = %q, want SummaryLine's %q", got, want)
	}
}

var allLefts = []Left{LeftNone, LeftClosed, LeftOpen, LeftOpenByChoice, LeftOpenByRule, LeftMayBeStarting}

// covers: MA-897, MA-925
// OutcomeLine (§4.6, C-43): every outcome in allOutcomes has its own line (a new
// outcome fails here until it has one), on both surfaces, in darwin and windows
// words, for every Left, with Minimized true and false. The lines are past tense
// and name no path or code; Windows words name the taskbar and the notification
// area, never the Dock or "out of sight"; desktop words name no command, CLI words
// no button; a line says "closed" only when Left is closed, and every left-open Left
// says so; Windows with Minimized false says the window may still be on screen. The
// design's never-close examples are pinned word for word.
func TestOutcomeLineEveryOutcome(t *testing.T) {
	for _, o := range allOutcomes {
		if o != OutcomeCancelled {
			if _, ok := outcomeHeads[o]; !ok {
				t.Errorf("%s has no OutcomeLine of its own", o.Name)
			}
		}
		for _, surface := range []string{SurfaceDesktop, SurfaceCLI} {
			for _, goos := range []string{"darwin", "windows"} {
				for _, left := range allLefts {
					for _, minimized := range []bool{true, false} {
						line := OutcomeLine(o, surface, goos, left, 6*time.Minute, minimized)
						ctx := o.Name + "/" + surface + "/" + goos + "/" + string(left) + "/min=" + strconv.FormatBool(minimized)
						checkOutcomeLine(t, ctx, line, o, surface, goos, left, minimized)
					}
				}
			}
		}
	}

	for _, g := range []struct {
		o    Outcome
		goos string
		left Left
		min  bool
		want string
	}{
		{OutcomeOK, "darwin", LeftOpenByRule, false,
			"Thunderbird synced: its mail settled after 6 min. It was left open out of sight; quit it from the Dock when you are done."},
		{OutcomeNoActivity, "darwin", LeftOpenByRule, false,
			"Thunderbird started, but nothing arrived in 20 minutes: either there was no new mail, or Thunderbird is waiting for a password or sign-in. To check, open Thunderbird from the Dock; it was left open."},
		{OutcomeWaitEnded, "darwin", LeftOpenByRule, false,
			"Thunderbird sync: you stopped the wait after 6 min, so the backup read the mail that had arrived by then; mail still arriving will be in the next backup. Thunderbird was left open out of sight; quit it from the Dock when you are done."},
		{OutcomeCancelled, "darwin", LeftOpen, false,
			"Thunderbird sync: cancelled — Thunderbird, started by this backup, was left open out of sight. Quit it from the Dock before the next Run now."},
		{OutcomeCancelled, "darwin", LeftMayBeStarting, false,
			"Thunderbird sync: cancelled — Thunderbird may still be starting; quit it from the Dock."},
		{OutcomeOK, "windows", LeftOpenByRule, true,
			"Thunderbird synced: its mail settled after 6 min. It was left open minimized to the taskbar; quit it from the taskbar or the notification area when you are done."},
		{OutcomeOK, "windows", LeftOpenByRule, false,
			"Thunderbird synced: its mail settled after 6 min. It was left open; its window may still be on screen; quit it from the taskbar or the notification area when you are done."},
	} {
		if got := OutcomeLine(g.o, SurfaceDesktop, g.goos, g.left, 6*time.Minute, g.min); got != g.want {
			t.Errorf("OutcomeLine(%s, desktop, %s, %s, min=%v) =\n  %q\nwant\n  %q", g.o.Name, g.goos, g.left, g.min, got, g.want)
		}
	}
}

func checkOutcomeLine(t *testing.T, ctx, line string, o Outcome, surface, goos string, left Left, minimized bool) {
	t.Helper()
	if strings.TrimSpace(line) == "" || !strings.Contains(line, "Thunderbird") {
		t.Errorf("%s: empty or Thunderbird-less line %q", ctx, line)
		return
	}
	if strings.Contains(line, "presync_") || strings.Contains(line, "/") || strings.Contains(line, `\`) {
		t.Errorf("%s: the line names a code or a path: %q", ctx, line)
	}
	if goos == "windows" && (strings.Contains(line, "Dock") || strings.Contains(line, "out of sight")) {
		t.Errorf("%s: a Windows line names the Dock or 'out of sight': %q", ctx, line)
	}
	if goos == "darwin" && (strings.Contains(line, "taskbar") || strings.Contains(line, "notification area")) {
		t.Errorf("%s: a macOS line names the taskbar: %q", ctx, line)
	}
	if surface == SurfaceDesktop && (strings.Contains(line, "mailarchive") || strings.Contains(line, "`")) {
		t.Errorf("%s: a desktop line names a command: %q", ctx, line)
	}
	if surface == SurfaceCLI && (strings.Contains(line, "Run now") || strings.Contains(line, "click")) {
		t.Errorf("%s: a command-line line names a button: %q", ctx, line)
	}
	closedClaim := strings.Contains(line, "was closed") || strings.Contains(line, "and closed")
	switch left {
	case LeftClosed:
		if !closedClaim || strings.Contains(line, "left open") {
			t.Errorf("%s: Left=closed but the line does not say it was closed: %q", ctx, line)
		}
	case LeftOpen, LeftOpenByRule, LeftOpenByChoice:
		if closedClaim || !strings.Contains(line, "left open") {
			t.Errorf("%s: a left-open Left but the line does not say left open: %q", ctx, line)
		}
		if goos == "windows" && !minimized && left != LeftOpenByChoice && !strings.Contains(line, "may still be on screen") {
			t.Errorf("%s: Windows, never minimized, but the line does not say the window may still be on screen: %q", ctx, line)
		}
	case LeftMayBeStarting:
		if closedClaim || !strings.Contains(line, "may still be starting") {
			t.Errorf("%s: Left=may-be-starting but the line does not say so: %q", ctx, line)
		}
	case LeftNone:
		if closedClaim || strings.Contains(line, "left open") {
			t.Errorf("%s: Left=none but the line claims a close or a left-open: %q", ctx, line)
		}
	}
	if o == OutcomeNoActivity || o == OutcomeWatchTruncatedNoActivity {
		if !strings.Contains(line, "nothing arrived") || !strings.Contains(line, "password or sign-in") ||
			strings.Contains(line, "synced first") || strings.Contains(line, "Thunderbird synced") {
			t.Errorf("%s: the no-change line must say nothing arrived and name a password or sign-in, never 'synced': %q", ctx, line)
		}
	}
}

// covers: MA-922, MA-929
// Synced() (A8, §5.2, C-72) reads the outcome alone — never Left: the synced rows
// are synced, a cancelled run is never synced whatever Thunderbird was left as, and
// a wait the person stopped (wait-ended) counts neither way, so it is not synced.
func TestOutcomeSynced(t *testing.T) {
	synced := map[Outcome]bool{
		OutcomeOK: true, OutcomeWatchTruncated: true, OutcomeNotQuiet: true, OutcomeQuitNotPermitted: true,
		OutcomeQuitTimeout: true, OutcomeQuitSkippedOthers: true, OutcomeQuitNoWindow: true, OutcomeOwnerActive: true,
		OutcomeIdentityUnavailable: true, OutcomeNoActivity: true, OutcomeWatchTruncatedNoActivity: true,
		OutcomeAlreadyRunning: true,
	}
	for _, o := range allOutcomes {
		if got := o.Synced(); got != synced[o] {
			t.Errorf("%s.Synced() = %v, want %v (§5.2)", o.Name, got, synced[o])
		}
	}
	if OutcomeCancelled.Synced() || OutcomeWaitEnded.Synced() {
		t.Error("a cancelled or stopped-wait run must never count as synced")
	}
}

// cancelQuit returns a context the test cancels with the Cancel button's cause.
func cancelQuit() (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(context.Background())
	return ctx, func() { cancel(ErrCancelAndQuit) }
}

// covers: MA-922
// The never-close rule in the session (A12, A4(ii), C-72, C-74, OQ-1). At quiet
// and at the 20-minute limit: zero quit requests, ownership re-verified, the
// record cleared, Left left-open-by-rule, the S7 verdict as the outcome; when
// ownership can no longer be proven the record is kept for the next run instead.
// Cancel (ErrCancelAndQuit) in S7: zero requests, left-open-by-rule, cancelled,
// not synced; any other cancel in S7 keeps today's left-running record (A4(i)).
// Cancel in S6: the child poll runs on to the 90 s limit — a child proven is
// re-verified, its record cleared, left-open-by-rule, with phase `cancelling`
// reported; none proven leaves `may-be-starting` and the `launching` record; a
// Stop during the poll ends it at once. The next session then sees the open
// Thunderbird and leaves it alone (already-running).
func TestNeverCloseRule(t *testing.T) {
	t.Run("at quiet, then the next session leaves it alone", func(t *testing.T) {
		f := attendedFake()
		sp := filepath.Join(t.TempDir(), "s.json")
		var pl phaseLog
		d := attendedDeps(f, sp)
		d.phase = pl.fn
		res := runSession(context.Background(), d)
		if res.outcome != OutcomeOK || f.quitCalls != 0 || res.left != LeftOpenByRule || !recordGone(t, sp) {
			t.Fatalf("outcome %q, %d quits, Left %q, record gone %v; want ok, 0, left-open-by-rule, cleared",
				res.outcome.Name, f.quitCalls, res.left, recordGone(t, sp))
		}
		if got := pl.got(); len(got) != 2 || got[0] != PhaseStarting || got[1] != PhaseWaiting {
			t.Errorf("phases = %v, want [starting waiting]", got)
		}
		next := runSession(context.Background(), attendedDeps(f, sp))
		if next.outcome != OutcomeAlreadyRunning || f.startCalls != 1 || f.quitCalls != 0 {
			t.Errorf("next session: outcome %q, %d launches, %d quits; want already-running, 1, 0", next.outcome.Name, f.startCalls, f.quitCalls)
		}
	})

	// The child exits on its own just before the ownership re-check (gate-2
	// adversarial pass 1, as1b-2) — the person quit it from the Dock inside one poll
	// gap — at quiet (A12), at "Stop waiting" (A16) and at a Cancel (A4(ii)): nothing
	// is left open, so the record is cleared and Left is none; never "left open, quit
	// it" for a Thunderbird that is not running. The twin, a child still running but
	// no longer provably ours, keeps its record (next subtest).
	for _, c := range []struct {
		name string
		set  func(f *fakeEnv, d *deps, cancel func())
		want Outcome
	}{
		{"at quiet", func(*fakeEnv, *deps, func()) {}, OutcomeOK},
		{"at a Cancel in S7", func(f *fakeEnv, _ *deps, cancel func()) { f.digestHook = cancel }, OutcomeCancelled},
		{"at Stop waiting", func(f *fakeEnv, d *deps, _ func()) {
			ew, endWait := context.WithCancel(context.Background())
			d.endWait = ew
			f.digestHook = endWait
		}, OutcomeWaitEnded},
	} {
		t.Run("the child gone "+c.name, func(t *testing.T) {
			ctx, cancel := cancelQuit()
			f := attendedFake()
			exitBeforeTheRecheck(f)
			sp := filepath.Join(t.TempDir(), "s.json")
			d := attendedDeps(f, sp)
			c.set(f, &d, cancel)
			res := runSession(ctx, d)
			if res.outcome != c.want || f.quitCalls != 0 {
				t.Errorf("outcome %q, %d quits; want %q, 0", res.outcome.Name, f.quitCalls, c.want.Name)
			}
			if res.left != LeftNone || !recordGone(t, sp) {
				t.Errorf("Left %q, record gone %v; want none and cleared: no Thunderbird is running", res.left, recordGone(t, sp))
			}
		})
	}

	t.Run("ownership not proven at quiet keeps the record", func(t *testing.T) {
		f := attendedFake()
		loseOwnershipAfterAdoption(f)
		sp := filepath.Join(t.TempDir(), "s.json")
		res := runSession(context.Background(), attendedDeps(f, sp))
		if f.quitCalls != 0 || res.left != LeftOpen || recordGone(t, sp) {
			t.Errorf("%d quits, Left %q, record gone %v; want 0, left-open, the record kept for the next run",
				f.quitCalls, res.left, recordGone(t, sp))
		}
	})

	for _, c := range []struct {
		name   string
		change time.Time
		want   Outcome
	}{
		{"at the 20-minute limit, nothing arrived", as1aBase.Add(24 * time.Hour), OutcomeNoActivity},
		{"at the 20-minute limit, mail still arriving", time.Time{}, OutcomeNotQuiet},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := attendedFake()
			if c.change.IsZero() {
				f.digestHook = func() { f.dgChanged++ } // a new digest at every poll: never quiet
				f.changeAt = as1aBase
			} else {
				f.changeAt = c.change
			}
			sp := filepath.Join(t.TempDir(), "s.json")
			res := runSession(context.Background(), attendedDeps(f, sp))
			if res.outcome != c.want || res.verdict != c.want || f.quitCalls != 0 || res.left != LeftOpenByRule || !recordGone(t, sp) {
				t.Errorf("outcome %q verdict %q, %d quits, Left %q, record gone %v; want %q twice, 0, left-open-by-rule, cleared",
					res.outcome.Name, res.verdict.Name, f.quitCalls, res.left, recordGone(t, sp), c.want.Name)
			}
		})
	}

	t.Run("Cancel in S7", func(t *testing.T) {
		ctx, cancel := cancelQuit()
		f := attendedFake()
		f.digestHook = cancel
		sp := filepath.Join(t.TempDir(), "s.json")
		res := runSession(ctx, attendedDeps(f, sp))
		if res.outcome != OutcomeCancelled || f.quitCalls != 0 || res.left != LeftOpenByRule || !recordGone(t, sp) || res.outcome.Synced() {
			t.Errorf("outcome %q, %d quits, Left %q, record gone %v, synced %v; want cancelled, 0, left-open-by-rule, cleared, not synced",
				res.outcome.Name, f.quitCalls, res.left, recordGone(t, sp), res.outcome.Synced())
		}
	})

	t.Run("another cancel cause in S7 (A4(i))", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		f := attendedFake()
		f.digestHook = cancel
		sp := filepath.Join(t.TempDir(), "s.json")
		res := runSession(ctx, attendedDeps(f, sp))
		rec, present, _ := loadRecord(sp)
		if res.outcome != OutcomeCancelled || f.quitCalls != 0 || res.left != LeftOpen || !present || rec.State != stateLeftRunning {
			t.Errorf("outcome %q, %d quits, Left %q, record %v/%q; want cancelled, 0, left-open, left-running",
				res.outcome.Name, f.quitCalls, res.left, present, rec.State)
		}
	})

	t.Run("Cancel in S6, the child proven during the poll", func(t *testing.T) {
		ctx, cancel := cancelQuit()
		f := attendedFake()
		f.appearIn = 10 * time.Second
		f.probeHook = func(name string) {
			if name == "procs" && f.started {
				cancel()
			}
		}
		sp := filepath.Join(t.TempDir(), "s.json")
		var pl phaseLog
		d := attendedDeps(f, sp)
		d.phase = pl.fn
		res := runSession(ctx, d)
		if res.outcome != OutcomeCancelled || f.quitCalls != 0 || res.left != LeftOpenByRule || !recordGone(t, sp) {
			t.Errorf("outcome %q, %d quits, Left %q, record gone %v; want cancelled, 0, left-open-by-rule, cleared",
				res.outcome.Name, f.quitCalls, res.left, recordGone(t, sp))
		}
		if got := pl.got(); len(got) != 2 || got[0] != PhaseStarting || got[1] != PhaseCancelling {
			t.Errorf("phases = %v, want [starting cancelling]", got)
		}
	})

	// The S6 Cancel poll proves the child, and the ownership re-check right after it
	// fails (gate-2 adversarial pass 1, as1b-3, as1b-2): a second candidate appears —
	// the record kept holds the proven child's identity (`syncing`, its pid and start),
	// never a pid-less `launching` record that the next run's weak match would pin on
	// the person's own Thunderbird (SAF-5); or the child has gone — nothing is left
	// open, the record is cleared and Left is none.
	for _, gone := range []bool{false, true} {
		name := "Cancel in S6, the child proven, then not provable at the re-check"
		if gone {
			name = "Cancel in S6, the child proven, then gone at the re-check"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := cancelQuit()
			f := attendedFake()
			f.appearIn = 10 * time.Second
			cancelled, proven := false, false
			f.probeHook = func(name string) {
				if name != "procs" || !f.started {
					return
				}
				switch {
				case !cancelled:
					cancelled = true
					cancel() // at the first S6 pass, before the child is up
				case f.nowT.Before(f.startedAt.Add(f.appearIn)):
				case !proven:
					proven = true // the poll's listing that proves it
				case gone:
					f.quitted, f.quitAt, f.exitIn = true, f.nowT, 0
				default:
					twin := f.child
					twin.pid = 201
					f.extraProcs = []proc{twin}
				}
			}
			sp := filepath.Join(t.TempDir(), "s.json")
			res := runSession(ctx, attendedDeps(f, sp))
			if !proven || res.outcome != OutcomeCancelled || f.quitCalls != 0 {
				t.Fatalf("proven %v, outcome %q, %d quits; want the child proven, cancelled, 0", proven, res.outcome.Name, f.quitCalls)
			}
			rec, present, _ := loadRecord(sp)
			if gone {
				if res.left != LeftNone || present {
					t.Errorf("Left %q, record kept %v; want none and cleared", res.left, present)
				}
				return
			}
			if res.left != LeftOpen || !present || rec.State != stateSyncing || rec.ChildPID != f.child.pid || rec.ChildStart != f.child.start.Unix() {
				t.Errorf("Left %q, record %v state %q pid %d start %d; want left-open and a syncing record with the child's pid %d and start %d",
					res.left, present, rec.State, rec.ChildPID, rec.ChildStart, f.child.pid, f.child.start.Unix())
			}
		})
	}

	t.Run("Cancel in S6, no child by the 90 s limit", func(t *testing.T) {
		ctx, cancel := cancelQuit()
		f := attendedFake()
		f.appearIn = 24 * time.Hour
		f.probeHook = func(name string) {
			if name == "procs" && f.started {
				cancel()
			}
		}
		sp := filepath.Join(t.TempDir(), "s.json")
		res := runSession(ctx, attendedDeps(f, sp))
		rec, present, _ := loadRecord(sp)
		if res.outcome != OutcomeCancelled || res.left != LeftMayBeStarting || !present || rec.State != stateLaunching {
			t.Errorf("outcome %q, Left %q, record %v/%q; want cancelled, may-be-starting, the launching record kept",
				res.outcome.Name, res.left, present, rec.State)
		}
		if waited := f.nowT.Sub(f.startedAt); waited < 90*time.Second || waited > 92*time.Second {
			t.Errorf("the child poll stopped after %v, want it to run on to the 90 s await limit and no further (C-74)", waited)
		}
	})

	t.Run("Stop during the S6 child poll", func(t *testing.T) {
		ctx, cancel := cancelQuit()
		stop, cancelStop := context.WithCancel(context.Background())
		defer cancelStop()
		f := attendedFake()
		f.appearIn = 24 * time.Hour
		n := 0
		f.probeHook = func(name string) {
			if name == "procs" && f.started {
				if n++; n == 1 {
					cancel()
				} else if n == 3 {
					cancelStop()
				}
			}
		}
		sp := filepath.Join(t.TempDir(), "s.json")
		d := attendedDeps(f, sp)
		d.sctx = stop
		res := runSession(ctx, d)
		if res.outcome != OutcomeCancelled || res.left != LeftMayBeStarting {
			t.Errorf("outcome %q, Left %q; want cancelled, may-be-starting", res.outcome.Name, res.left)
		}
		if waited := f.nowT.Sub(f.startedAt); waited >= 90*time.Second {
			t.Errorf("the poll ran on for %v after the Stop, want it to end at once", waited)
		}
	})
}

// covers: MA-921
// The S6 Cancel child poll fits the budget (§2.6, C-74, C-50): from the same
// constants, with every probe at or just under its bound, a stale lock, the
// launcher at its bound, and the Cancel landing at the first S6 pass, the session
// polls on to the 90 s await limit — whether the child appears at the very end or
// never — and that path, with resolve's two probes, fits the budget less its slack.
func TestSessionBudgetCoversTheS6CancelPoll(t *testing.T) {
	var longest, fromCancel time.Duration
	for _, probe := range []time.Duration{probeTimeout, probeTimeout - 100*time.Millisecond, probeTimeout * 9 / 10, probeTimeout / 2} {
		for _, appear := range []time.Duration{defAwaitUp - time.Second, defAwaitUp, 24 * time.Hour} {
			ctx, cancel := cancelQuit()
			f := attendedFake()
			f.lockOn[as1aProfile], f.lockP[as1aProfile] = true, 999
			f.probeCost, f.launchCost, f.quitCost = probe, launchTimeout, quitTimeout
			f.appearIn = appear
			var cancelAt time.Time
			f.probeHook = func(name string) {
				if name == "procs" && f.started {
					if cancelAt.IsZero() {
						cancelAt = f.nowT
					}
					cancel()
				}
			}
			d := attendedDeps(f, filepath.Join(t.TempDir(), "s.json"))
			d.cfg = quietConfig{settle: defSettle, quiet: defQuiet, maxWait: defMaxWait}
			d.awaitPoll, d.awaitUp, d.pollEvery, d.quitGrace = defAwaitPoll, defAwaitUp, defPollEvery, defQuitGrace
			res := runSession(ctx, d)
			if res.outcome != OutcomeCancelled {
				t.Fatalf("probe %v appear %v: outcome %q, want cancelled", probe, appear, res.outcome.Name)
			}
			if took := f.nowT.Sub(as1aBase) + 2*probe; took > longest {
				longest = took
			}
			if took := f.nowT.Sub(cancelAt); took > fromCancel {
				fromCancel = took
			}
		}
	}
	t.Logf("longest S6 Cancel path %v, budget %v; from the Cancel %v, cancel bound %v", longest, sessionBudget(), fromCancel, cancelPathBudget())
	if room := sessionBudget() - budgetSlack; longest > room {
		t.Errorf("the S6 Cancel child poll takes %v, more than the budget leaves before its slack (%v)", longest, room)
	}
	// as1b-1: the watchdog's wait after a Cancel (cancelPathBudget) covers the same
	// poll, measured from the Cancel, with its own slack still left over.
	if room := cancelPathBudget() - cancelSlack; fromCancel > room {
		t.Errorf("the S6 Cancel child poll takes %v from the Cancel, more than cancelPathBudget leaves before its slack (%v)", fromCancel, room)
	}
	if cancelPathWait != cancelPathBudget() {
		t.Errorf("cancelPathWait = %v, want cancelPathBudget() %v", cancelPathWait, cancelPathBudget())
	}
}

// covers: MA-921
// The watchdog and the S6 Cancel path (§2.6 (2), C-74): with the Cancel button's
// cause it WAITS for the session's bounded child poll (the session then returns
// its own result, left open by rule), while any other cancel cause, or a Stop
// during that poll, makes it return at once — cancelled, the session abandoned,
// Left taken from where the session was (Thunderbird may still be starting).
func TestWatchdogWaitsForTheS6CancelPoll(t *testing.T) {
	for _, c := range []struct {
		name     string
		cause    error
		stopPoll bool
		waits    bool
		wantLeft Left
	}{
		{"Cancel: the watchdog waits for the poll", ErrCancelAndQuit, false, true, LeftOpenByRule},
		{"another cancel cause: at once", errors.New("interrupted"), false, false, LeftMayBeStarting},
		{"a Stop during the poll: at once", ErrCancelAndQuit, true, false, LeftMayBeStarting},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			stop, cancelStop := context.WithCancel(context.Background())
			defer cancelStop()
			f := attendedFake()
			f.appearIn = 10 * time.Second
			blocked, unblock := make(chan struct{}), make(chan struct{})
			n := 0
			f.probeHook = func(name string) {
				if name != "procs" || !f.started {
					return
				}
				n++
				switch {
				case n == 1 && !c.stopPoll:
					cancel(c.cause)
					close(blocked)
					<-unblock
				case n == 1:
					cancel(c.cause)
				case n == 2 && c.stopPoll:
					cancelStop()
					close(blocked)
					<-unblock
				}
			}
			sp := filepath.Join(t.TempDir(), "s.json")
			returned, sessionDone := make(chan struct{}), make(chan struct{})
			var res Result
			go func() {
				defer close(returned)
				res = runWatchdog(ctx, stop, 30*time.Second, func(sc sessionCtl) Result {
					defer close(sessionDone)
					d := attendedDeps(f, sp)
					d.sctx, d.abandoned, d.markLeft = sc.ctx, sc.abandoned, sc.markLeft
					return runSession(ctx, d).result()
				})
			}()
			select {
			case <-blocked:
			case <-sessionDone:
				close(unblock)
				t.Fatal("the session ended without reaching the point this case holds it at")
			case <-time.After(10 * time.Second):
				close(unblock)
				t.Fatal("the session never reached the point this case holds it at")
			}
			select {
			case <-returned:
				if c.waits {
					t.Error("the watchdog returned while the session's Cancel child poll was still running")
				}
			case <-time.After(300 * time.Millisecond):
				if !c.waits {
					t.Error("the watchdog did not return at once")
				}
			}
			close(unblock)
			<-sessionDone
			<-returned
			if res.Outcome != OutcomeCancelled || res.Left != c.wantLeft {
				t.Errorf("watchdog result %q, Left %q; want cancelled, %q", res.Outcome.Name, res.Left, c.wantLeft)
			}
			if res.Release != nil {
				res.Release()
			}
		})
	}
}

// covers: MA-921
// After the person's Cancel the watchdog waits for the session's own Cancel path
// only up to that path's bound, cancelPathWait (gate-2 adversarial pass 1, as1b-1),
// never the whole session budget: a Cancel while the S7 folder walk is blocked (a
// stalled disk; a walk honours no context) returns `cancelled` once that bound is
// spent, with Left from where the session was (left open), the session abandoned and
// nothing asked to quit. Positive twin: TestWatchdogWaitsForTheS6CancelPoll, whose
// Cancel case is waited for inside the bound, and TestSessionBudgetCoversTheS6CancelPoll,
// which drives that poll inside cancelPathBudget().
func TestWatchdogCancelWaitIsBounded(t *testing.T) {
	old := cancelPathWait
	t.Cleanup(func() { cancelPathWait = old })
	cancelPathWait = 200 * time.Millisecond

	ctx, cancel := cancelQuit()
	f := attendedFake()
	blocked, unblock := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.digestHook = func() {
		once.Do(func() {
			cancel()
			close(blocked)
			<-unblock // the walk stalls and honours no context
		})
	}
	returned, sessionDone := make(chan struct{}), make(chan struct{})
	var res Result
	var ctl sessionCtl
	go func() {
		defer close(returned)
		res = runWatchdog(ctx, nil, 5*time.Second, func(sc sessionCtl) Result {
			defer close(sessionDone)
			ctl = sc
			d := attendedDeps(f, filepath.Join(t.TempDir(), "s.json"))
			sc.wire(&d)
			return runSession(ctx, d).result()
		})
	}()
	<-blocked
	start := time.Now()
	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		close(unblock)
		t.Fatal("the watchdog did not return")
	}
	took := time.Since(start)
	close(unblock)
	<-sessionDone
	if took > 2*time.Second {
		t.Errorf("the watchdog held the Cancel %v, want about cancelPathWait (200ms), not its 5s budget", took)
	}
	if res.Outcome != OutcomeCancelled || res.Left != LeftOpen {
		t.Errorf("watchdog result %q, Left %q; want cancelled, left-open", res.Outcome.Name, res.Left)
	}
	if !ctl.isAbandoned() || f.quitCalls != 0 {
		t.Errorf("abandoned %v, %d quit requests; want abandoned and 0", ctl.isAbandoned(), f.quitCalls)
	}
	if res.Release != nil {
		res.Release()
	}
}

// covers: MA-925
// No-change honesty (A13, C-68), both modes: a session in which nothing arrived
// ends ok-noactivity, and a truncated walk with nothing arriving ends the new
// watch-truncated-noactivity — never watch-truncated, which says it synced.
// Positive twin first: a truncated walk in which mail did arrive stays
// watch-truncated.
func TestNoChangeHonesty(t *testing.T) {
	for _, attended := range []bool{false, true} {
		run := func(change time.Time, truncated bool) Outcome {
			f := syncingFake()
			f.changeAt, f.digestTruncated = change, truncated
			d := syncingDeps(f, filepath.Join(t.TempDir(), "s.json"))
			if attended {
				f.idle = 0
				d = attendedDeps(f, d.statePath)
			}
			return runSession(context.Background(), d).outcome
		}
		if got := run(as1aBase.Add(24*time.Second), true); got != OutcomeWatchTruncated {
			t.Fatalf("attended=%v twin: a truncated walk with mail arriving ended %q, want watch-truncated", attended, got.Name)
		}
		if got := run(as1aBase.Add(24*time.Hour), false); got != OutcomeNoActivity {
			t.Errorf("attended=%v: nothing arrived ended %q, want ok-noactivity", attended, got.Name)
		}
		if got := run(as1aBase.Add(24*time.Hour), true); got != OutcomeWatchTruncatedNoActivity {
			t.Errorf("attended=%v: a truncated walk with nothing arriving ended %q, want watch-truncated-noactivity", attended, got.Name)
		}
	}
	if OutcomeWatchTruncatedNoActivity.Class != ClassNote {
		t.Errorf("watch-truncated-noactivity is %s, want a note", OutcomeWatchTruncatedNoActivity.Class)
	}
	for _, o := range []Outcome{OutcomeNoActivity, OutcomeWatchTruncatedNoActivity} {
		if s := SummaryLine(o); strings.Contains(s, "synced first") || !strings.Contains(s, "nothing new") {
			t.Errorf("%s summary %q must say nothing new arrived", o.Name, s)
		}
	}
}

// covers: MA-929
// "Stop waiting and back up now" in the session (A16, OQ-6). Positive twin
// first: an attended session whose EndWait never ends runs to quiet. EndWait
// ending in S7 ends the wait AT ONCE (the fake clock never reaches quiet): zero
// quit requests — also with NeverQuit false — ownership re-verified, the record
// cleared, outcome wait-ended, Verdict zero, Left left-open-by-rule, not synced.
// EndWait ending in S5 or S6 changes nothing; a Cancel or a Stop that came first
// wins; and a scheduled session ignores an EndWait it was handed.
func TestStopWaitingEndsTheWait(t *testing.T) {
	var endedAt time.Time
	endWaitAtDigest := func(f *fakeEnv, n int, also func()) context.Context {
		ew, end := context.WithCancel(context.Background())
		k := 0
		f.digestHook = func() {
			if k++; k == n {
				if also != nil {
					also()
				}
				end()
				endedAt = f.nowT
			}
		}
		return ew
	}

	f := attendedFake()
	d := attendedDeps(f, filepath.Join(t.TempDir(), "s.json"))
	d.endWait = context.Background()
	if res := runSession(context.Background(), d); res.outcome != OutcomeOK {
		t.Fatalf("twin: outcome %q, want ok at quiet", res.outcome.Name)
	}

	for _, neverQuit := range []bool{true, false} {
		f := attendedFake()
		sp := filepath.Join(t.TempDir(), "s.json")
		d := attendedDeps(f, sp)
		d.neverQuit = neverQuit
		d.endWait = endWaitAtDigest(f, 2, nil)
		res := runSession(context.Background(), d)
		if res.outcome != OutcomeWaitEnded || f.quitCalls != 0 || res.verdict != (Outcome{}) || res.left != LeftOpenByRule ||
			!recordGone(t, sp) || res.outcome.Synced() {
			t.Errorf("neverQuit=%v: outcome %q, %d quits, verdict %q, Left %q, record gone %v, synced %v; want wait-ended, 0, none, left-open-by-rule, cleared, false",
				neverQuit, res.outcome.Name, f.quitCalls, res.verdict.Name, res.left, recordGone(t, sp), res.outcome.Synced())
		}
		if res.waited >= 30*time.Second || !f.nowT.Equal(endedAt) {
			t.Errorf("neverQuit=%v: the session ran on %v after EndWait ended (waited %v), want it to end at once with no further poll",
				neverQuit, f.nowT.Sub(endedAt), res.waited)
		}
	}

	t.Run("EndWait ending in S5 or S6 changes nothing", func(t *testing.T) {
		for _, at := range []string{"S5", "S6"} {
			f := attendedFake()
			ew, end := context.WithCancel(context.Background())
			n := 0
			f.probeHook = func(name string) {
				if name != "procs" {
					return
				}
				n++
				if (at == "S5" && n == 3) || (at == "S6" && f.started) {
					end()
				}
			}
			d := attendedDeps(f, filepath.Join(t.TempDir(), "s.json"))
			d.endWait = ew
			if res := runSession(context.Background(), d); res.outcome != OutcomeOK || res.verdict != OutcomeOK {
				t.Errorf("EndWait ended in %s: outcome %q, want the session to run on to ok", at, res.outcome.Name)
			}
		}
	})

	t.Run("a Cancel first wins", func(t *testing.T) {
		ctx, cancel := cancelQuit()
		f := attendedFake()
		d := attendedDeps(f, filepath.Join(t.TempDir(), "s.json"))
		d.endWait = endWaitAtDigest(f, 2, cancel)
		if res := runSession(ctx, d); res.outcome != OutcomeCancelled {
			t.Errorf("outcome %q, want cancelled (the Cancel came first)", res.outcome.Name)
		}
	})

	t.Run("a Stop first wins", func(t *testing.T) {
		stop, cancelStop := context.WithCancel(context.Background())
		f := attendedFake()
		d := attendedDeps(f, filepath.Join(t.TempDir(), "s.json"))
		d.sctx = stop
		d.endWait = endWaitAtDigest(f, 2, cancelStop)
		if res := runSession(context.Background(), d); res.outcome != OutcomeCancelled || f.quitCalls != 0 {
			t.Errorf("outcome %q, %d quits; want cancelled, 0 (the Stop came first)", res.outcome.Name, f.quitCalls)
		}
	})

	t.Run("a scheduled session ignores EndWait", func(t *testing.T) {
		f := syncingFake()
		d := syncingDeps(f, filepath.Join(t.TempDir(), "s.json"))
		d.endWait = endWaitAtDigest(f, 2, nil)
		if res := runSession(context.Background(), d); res.outcome != OutcomeOK || f.quitCalls != 1 {
			t.Errorf("outcome %q, %d quits; want ok and the scheduled quit", res.outcome.Name, f.quitCalls)
		}
	})
}

// covers: MA-922, MA-929
// The session reports its phases only while it is not abandoned (A15, C-71): a
// session the watchdog gave up on makes no Phase call afterwards, so a late call
// can never overwrite the next run's note. Positive twin first: a live session
// reports starting and waiting.
func TestNoPhaseAfterAbandoned(t *testing.T) {
	f := attendedFake()
	var pl phaseLog
	d := attendedDeps(f, filepath.Join(t.TempDir(), "s.json"))
	d.phase = pl.fn
	runSession(context.Background(), d)
	if len(pl.got()) == 0 {
		t.Fatal("twin: a live session reported no phase")
	}

	if got := pl.got(); len(got) != 2 || got[1] != PhaseWaiting {
		t.Fatalf("twin: phases %v, want [starting waiting]", got)
	}

	// The watchdog gives up at the very probe that adopts the child: the session's
	// next step would announce `waiting`.
	f = attendedFake()
	var pl2 phaseLog
	var gaveUp bool
	f.probeHook = func(name string) {
		if name == "procs" && f.started && !f.nowT.Before(f.startedAt.Add(f.appearIn)) {
			gaveUp = true
		}
	}
	d = attendedDeps(f, filepath.Join(t.TempDir(), "s.json"))
	d.phase = pl2.fn
	d.abandoned = func() bool { return gaveUp }
	runSession(context.Background(), d)
	if got := pl2.got(); len(got) != 1 || got[0] != PhaseStarting {
		t.Errorf("phases %v, want only [starting]: no Phase call once the session is abandoned (A15)", got)
	}
}

// labGateProblems lists every table entry set true that does not name a lab
// record under docs/lab/ that exists below root and names its platform (§2.7,
// §2.8, MA-927, C-66).
func labGateProblems(root string, tables map[string]map[string]labGate) []string {
	var probs []string
	for tname, table := range tables {
		for goos, g := range table {
			if !strings.HasPrefix(g.record, "docs/lab/") {
				probs = append(probs, tname+"/"+goos+": the lab record path is not under docs/lab/: "+g.record)
			}
			if !g.allowed {
				continue
			}
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(g.record)))
			if err != nil {
				probs = append(probs, tname+"/"+goos+": set true but its lab record is missing: "+g.record)
				continue
			}
			if !strings.Contains(string(data), goos) {
				probs = append(probs, tname+"/"+goos+": its lab record does not name the platform: "+g.record)
			}
		}
	}
	return probs
}

// repoRoot walks up from the package directory to the folder holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the package")
		}
		dir = parent
	}
}

// callsOverride reports whether Go source uses OverrideTablesForTest at all — a
// call, or the function taken as a value (as1b-4) — bare or as presync's selector.
// Only the name of its own top-level declaration is not a use.
func callsOverride(fset *token.FileSet, name string, src any) (bool, error) {
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return false, err
	}
	decl := map[*ast.Ident]bool{}
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
			decl[fd.Name] = true
		}
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "OverrideTablesForTest" && !decl[id] {
			found = true
		}
		return !found
	})
	return found, nil
}

// covers: MA-927
// The two per-platform tables (§2.7 AttendedStartAllowed, §2.8
// AttendedQuitAllowed; C-49, C-66, OQ-7): every entry names its lab record's path
// under docs/lab/, and an entry is true only when that record exists and names the
// platform. Positive twin first: a planted true entry whose record exists and names
// darwin passes; one with no record, or a record naming the other platform, fails.
// The lookups answer from the tables (an unknown platform is false), the one
// test-only override flips them and its restore puts them back, and no non-test Go
// file in the repository uses OverrideTablesForTest — calls it, or takes it as a
// value (as1b-4; planted twins of each caught first).
func TestAttendedTablesNeedALabRecord(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "lab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "lab", "x.md"), []byte("lab record, platform: darwin"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok := map[string]map[string]labGate{"start": {"darwin": {allowed: true, record: "docs/lab/x.md"}}}
	if p := labGateProblems(root, ok); len(p) != 0 {
		t.Fatalf("twin: a true entry with its record flagged: %v", p)
	}
	for name, bad := range map[string]map[string]map[string]labGate{
		"no record":          {"start": {"darwin": {allowed: true, record: "docs/lab/none.md"}}},
		"the other platform": {"quit": {"windows": {allowed: true, record: "docs/lab/x.md"}}},
		"not under docs/lab": {"quit": {"darwin": {record: "lab.md"}}},
	} {
		if p := labGateProblems(root, bad); len(p) == 0 {
			t.Errorf("planted %s: not caught", name)
		}
	}

	tables := map[string]map[string]labGate{"start": attendedStartTable, "quit": attendedQuitTable}
	for tname, table := range tables {
		for _, goos := range []string{"darwin", "windows"} {
			if _, ok := table[goos]; !ok {
				t.Errorf("%s table has no %s entry", tname, goos)
			}
		}
	}
	if p := labGateProblems(repoRoot(t), tables); len(p) != 0 {
		t.Errorf("the real tables: %v", p)
	}

	for _, goos := range []string{"darwin", "windows", "linux"} {
		if AttendedStartAllowed(goos) != attendedStartTable[goos].allowed || AttendedQuitAllowed(goos) != attendedQuitTable[goos].allowed {
			t.Errorf("%s: the lookups do not answer from the tables", goos)
		}
	}
	if AttendedStartAllowed("linux") || AttendedQuitAllowed("linux") {
		t.Error("an unknown platform must be false")
	}
	before := AttendedStartAllowed("darwin")
	restore := OverrideTablesForTest(map[string]bool{"darwin": true}, map[string]bool{"darwin": !before})
	if !AttendedQuitAllowed("darwin") || AttendedStartAllowed("darwin") == before {
		t.Error("OverrideTablesForTest did not flip the entries")
	}
	restore()
	if AttendedStartAllowed("darwin") != before || AttendedQuitAllowed("darwin") != attendedQuitTable["darwin"].allowed {
		t.Error("restore did not put the tables back")
	}

	fset := token.NewFileSet()
	if hit, err := callsOverride(fset, "planted.go", "package x\nfunc f() { presync.OverrideTablesForTest(nil, nil) }\n"); err != nil || !hit {
		t.Fatalf("twin: a planted call was not caught (hit %v, err %v)", hit, err)
	}
	// Any use of the function, not only a direct call (gate-2 adversarial pass 1,
	// as1b-4): taken as a value and called later, or handed to a helper, it flips a
	// table all the same. Its own declaration is the one place the name may appear.
	for name, src := range map[string]string{
		"taken as a value":        "package x\nvar setTables = presync.OverrideTablesForTest\nfunc f() { setTables(map[string]bool{\"darwin\": true}, nil) }\n",
		"handed to a helper":      "package x\nfunc f() { helper(presync.OverrideTablesForTest) }\n",
		"a bare value in-package": "package presync\nfunc f() { g := OverrideTablesForTest; g(nil, nil) }\n",
	} {
		if hit, err := callsOverride(fset, "planted.go", src); err != nil || !hit {
			t.Errorf("planted %s: not caught (hit %v, err %v)", name, hit, err)
		}
	}
	if hit, err := callsOverride(fset, "attended.go", "package presync\nfunc OverrideTablesForTest(start, quit map[string]bool) func() { return func() {} }\n"); err != nil || hit {
		t.Errorf("its own declaration was flagged (hit %v, err %v)", hit, err)
	}
	var offenders []string
	scanned := 0
	err := filepath.WalkDir(repoRoot(t), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		hit, perr := callsOverride(fset, path, nil)
		if perr != nil {
			return perr
		}
		if hit {
			offenders = append(offenders, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 50 {
		t.Fatalf("only %d Go files scanned: the walk is not reading the repository", scanned)
	}
	if len(offenders) != 0 {
		t.Errorf("non-test files call OverrideTablesForTest: %v", offenders)
	}
}
