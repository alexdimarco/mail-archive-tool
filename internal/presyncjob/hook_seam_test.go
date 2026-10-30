package presyncjob

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/presync"
)

// recorder is the scripted fake session the seam takes (design-presync-attended
// §2.2, C-41, C-79): Thunderbird is the external binary, so this is the mock
// CLAUDE.md allows. It records the whole Config it was handed and returns a chosen
// result; it starts nothing.
type recorder struct {
	calls int
	cfg   presync.Config
	res   presync.Result
	order *[]string
}

func (r *recorder) run(_ context.Context, cfg presync.Config) presync.Result {
	r.calls++
	r.cfg = cfg
	if r.order != nil {
		*r.order = append(*r.order, "session")
	}
	return r.res
}

// covers: MA-898, MA-580
// The shared hook's seam (design-presync-attended §2.2, slice AS3, C-41, C-49,
// OQ-1, OQ-6). SetHook is untagged: where the platform has no adapter it installs
// nothing (positive twin first: through the test seam it installs). A scheduled
// caller's session is handed Attended false, NeverQuit false and no EndWait, even
// when the caller passes one; an attended caller gets no hook at all where
// AttendedStartAllowed is false, and where it is true gets Attended, NeverQuit
// from AttendedQuitAllowed alone (both values) and its EndWait. The session gets
// the run's files, the profile, the state folder and the caller's words.
func TestSetHookSeam(t *testing.T) {
	// Production: the adapter platforms are supported, every other is not.
	wantSupported := runtime.GOOS == "darwin" || runtime.GOOS == "windows"
	if Supported() != wantSupported {
		t.Fatalf("Supported() = %v on %s, want %v", Supported(), runtime.GOOS, wantSupported)
	}
	if !wantSupported {
		var opts app.Options
		if err := SetHook(&opts, HookOptions{}); err != nil || opts.BeforeRead != nil {
			t.Fatalf("no adapter: SetHook installed a hook (%v) or failed (%v)", opts.BeforeRead != nil, err)
		}
	}

	rec := &recorder{res: presync.Result{Outcome: presync.OutcomeOK, Left: presync.LeftClosed}}
	restore := OverrideSessionForTest(rec.run, "/state/presync")
	defer restore()
	if !Supported() {
		t.Fatal("OverrideSessionForTest did not make the seam supported")
	}

	// Positive twin first: a scheduled caller gets a hook, and the session the facts.
	var logw bytes.Buffer
	stop, stopEnd := context.WithCancel(context.Background())
	defer stopEnd()
	endWait, endWaitEnd := context.WithCancel(context.Background())
	defer endWaitEnd()
	var opts app.Options
	if err := SetHook(&opts, HookOptions{Profile: "/p", Log: &logw, Surface: presync.SurfaceCLI, GOOS: "darwin", Stop: stop, EndWait: endWait}); err != nil {
		t.Fatalf("SetHook: %v", err)
	}
	if opts.BeforeRead == nil {
		t.Fatal("a scheduled caller got no hook")
	}
	opts.BeforeRead(context.Background(), []string{"/in/a", "/in/b"})
	c := rec.cfg
	if rec.calls != 1 || len(c.Inputs) != 2 || c.Inputs[1] != "/in/b" || c.Profile != "/p" || !c.ProfileNamed ||
		c.StateDir != "/state/presync" || c.Log != &logw || c.Surface != presync.SurfaceCLI || c.GOOS != "darwin" || c.Stop != stop {
		t.Errorf("the session was handed %+v (calls %d)", c, rec.calls)
	}
	if c.Attended || c.NeverQuit || c.EndWait != nil {
		t.Errorf("a scheduled session was handed Attended %v, NeverQuit %v, EndWait %v; want false, false, nil", c.Attended, c.NeverQuit, c.EndWait)
	}

	// No profile named: the session derives it and requires profiles.ini (PC3).
	opts = app.Options{}
	if err := SetHook(&opts, HookOptions{}); err != nil {
		t.Fatal(err)
	}
	opts.BeforeRead(context.Background(), nil)
	if rec.cfg.ProfileNamed || rec.cfg.Profile != "" || rec.cfg.GOOS != runtime.GOOS {
		t.Errorf("no profile: handed Profile %q, ProfileNamed %v, GOOS %q", rec.cfg.Profile, rec.cfg.ProfileNamed, rec.cfg.GOOS)
	}

	// Attended, start not allowed on the platform (C-49): no hook, no error.
	for _, goos := range []string{"darwin", "windows"} {
		r := presync.OverrideTablesForTest(map[string]bool{goos: true}, map[string]bool{goos: false})
		opts = app.Options{}
		err := SetHook(&opts, HookOptions{Attended: true, GOOS: goos})
		r()
		if err != nil || opts.BeforeRead != nil {
			t.Errorf("%s, start not allowed: a hook was installed (%v) or SetHook failed (%v)", goos, opts.BeforeRead != nil, err)
		}
	}

	// Attended, start allowed: Attended, NeverQuit from the quit table alone, EndWait.
	for _, goos := range []string{"darwin", "windows"} {
		for _, quitAllowed := range []bool{false, true} {
			r := presync.OverrideTablesForTest(map[string]bool{goos: quitAllowed}, map[string]bool{goos: true})
			opts = app.Options{}
			if err := SetHook(&opts, HookOptions{Attended: true, GOOS: goos, Surface: presync.SurfaceDesktop, EndWait: endWait}); err != nil {
				r()
				t.Fatal(err)
			}
			r()
			if opts.BeforeRead == nil {
				t.Fatalf("%s: an attended caller where the start is allowed got no hook", goos)
			}
			opts.BeforeRead(context.Background(), nil)
			c := rec.cfg
			if !c.Attended || c.NeverQuit != !quitAllowed || c.EndWait != endWait || c.GOOS != goos {
				t.Errorf("%s, quit allowed %v: handed Attended %v, NeverQuit %v, EndWait set %v, GOOS %q",
					goos, quitAllowed, c.Attended, c.NeverQuit, c.EndWait == endWait, c.GOOS)
			}
		}
	}
}

// covers: MA-898, MA-901, MA-580
// The hook builds what the run hands its caller from the session's result
// (design-presync-attended §4.1, §4.6, C-12): every warning (the main one and each
// side one, B6), the session's Release, and the in-memory outcome — code, name,
// class, Synced (from the outcome alone), Left, Minimized, the verdict's name,
// Started, Waited and the end line in the caller's words. OnStart runs before the
// session and OnEnd after it, with that outcome. A state folder that cannot be
// found fails SetHook and installs nothing (the caller logs it and runs without
// the sync, R32).
func TestHookBuildsTheOutcome(t *testing.T) {
	released := false
	started := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	var order []string
	rec := &recorder{order: &order, res: presync.Result{
		Outcome: presync.OutcomeQuitNotPermitted,
		Side:    []presync.Outcome{presync.OutcomeStateUnreadable},
		Release: func() { released = true },
		Verdict: presync.OutcomeOK,
		Left:    presync.LeftOpen,
		Started: started,
		Waited:  4 * time.Minute,
	}}
	restore := OverrideSessionForTest(rec.run, "/state/presync")
	defer restore()

	var ended *app.PresyncOutcome
	var opts app.Options
	err := SetHook(&opts, HookOptions{
		Surface: presync.SurfaceCLI, GOOS: "windows",
		OnStart: func() { order = append(order, "start") },
		OnEnd:   func(o app.PresyncOutcome) { order = append(order, "end"); ended = &o },
	})
	if err != nil || opts.BeforeRead == nil {
		t.Fatalf("SetHook: hook %v, err %v", opts.BeforeRead != nil, err)
	}
	br := opts.BeforeRead(context.Background(), nil)

	if len(br.Warnings) != 2 || br.Warnings[0].Code != presync.OutcomeQuitNotPermitted.Code ||
		br.Warnings[1].Code != presync.OutcomeStateUnreadable.Code || br.Warnings[1].Message == "" {
		t.Errorf("warnings = %+v, want the main warning then the B6 side warning", br.Warnings)
	}
	if br.Release == nil {
		t.Fatal("the session's Release was not handed to the run")
	}
	br.Release()
	if !released {
		t.Error("the Release handed to the run is not the session's")
	}
	want := app.PresyncOutcome{
		Code: presync.OutcomeQuitNotPermitted.Code, Name: "quit-not-permitted", Class: "warn",
		Synced: presync.OutcomeQuitNotPermitted.Synced(), Left: "left-open", Verdict: "ok",
		Line:    presync.OutcomeLine(presync.OutcomeQuitNotPermitted, presync.SurfaceCLI, "windows", presync.LeftOpen, 4*time.Minute, false),
		Started: started, Waited: 4 * time.Minute,
	}
	if br.Outcome == nil || *br.Outcome != want {
		t.Errorf("outcome = %+v,\nwant %+v", br.Outcome, want)
	}
	if len(order) != 3 || order[0] != "start" || order[1] != "session" || order[2] != "end" {
		t.Errorf("order = %v, want start, session, end", order)
	}
	if ended == nil || *ended != want {
		t.Errorf("OnEnd got %+v, want the run's outcome", ended)
	}

	// A session that left nothing says none, and an ok one is synced.
	rec.res = presync.Result{Outcome: presync.OutcomeOK}
	br = opts.BeforeRead(context.Background(), nil)
	if br.Outcome == nil || br.Outcome.Left != "none" || !br.Outcome.Synced || br.Outcome.Class != "none" || len(br.Warnings) != 0 {
		t.Errorf("an ok session: outcome %+v, warnings %v", br.Outcome, br.Warnings)
	}

	// The state folder cannot be found: an error, and no hook.
	stateDirMu.Lock()
	oldDir := stateDirFn
	stateDirFn = func() (string, error) { return "", errors.New("no home") }
	stateDirMu.Unlock()
	defer func() { stateDirMu.Lock(); stateDirFn = oldDir; stateDirMu.Unlock() }()
	opts = app.Options{}
	if err := SetHook(&opts, HookOptions{}); err == nil || opts.BeforeRead != nil {
		t.Errorf("no state folder: err %v, hook installed %v; want an error and no hook", err, opts.BeforeRead != nil)
	}
}
