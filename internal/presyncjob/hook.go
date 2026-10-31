// Package presyncjob assembles the Thunderbird pre-sync as app.Run's BeforeRead
// hook, in ONE place shared by every surface that starts a local archive run
// which may pre-sync: the -thunderbird-sync CLI flag, the scheduled GUI job
// (internal/wizard.RunHeadless) and the Desktop's "Run now"
// (internal/wizard.RunJobInProcess). So the pre-sync behaves identically however
// the run was started (design docs/design-thunderbird-presync.md §4.1, §4.2.5;
// design-presync-attended §2.2, T20).
//
// SetHook is untagged (design-presync-attended §2.2, C-41): on macOS and Windows
// the adapter file switches the seam on; elsewhere it installs nothing, so a job
// that somehow carries the setting there is inert, not an error — the archive
// still runs (R32).
package presyncjob

import (
	"context"
	"errors"
	"io"
	"runtime"
	"sync"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/presync"
)

// HookOptions is what a caller hands SetHook (design-presync-attended §2.2).
// Attended is the caller's own: true only for a run the person started and is
// watching at this computer (R38); it is never read from a file. NeverQuit is not
// here: SetHook sets it from presync.AttendedQuitAllowed, so no caller can switch
// the close back on (OQ-1).
type HookOptions struct {
	// Profile is an explicit -thunderbird-profile or job profile; "" means derive
	// it from the inputs and require profiles.ini membership (PC3).
	Profile string
	// Log receives the owner-facing narration, one whole line per write.
	Log io.Writer
	// Attended: the person started this run at this computer (R38).
	Attended bool
	// Surface picks the end line's words (presync.SurfaceDesktop or SurfaceCLI).
	Surface string
	// GOOS picks the platform words and the tables' entry; "" means runtime.GOOS.
	GOOS string
	// Stop ends the session at once with no quit request (a Lock, Shut down).
	Stop context.Context
	// Phase is told the session's phases (§4.6).
	Phase func(phase string, since time.Time)
	// KeepOpen is the "Keep Thunderbird open" press (slice AS-Q).
	KeepOpen func() bool
	// EndWait is "Stop waiting and back up now"; handed on only when Attended (OQ-6).
	EndWait context.Context
	// OnStart runs right before the session, OnEnd right after it with the outcome.
	OnStart func()
	OnEnd   func(app.PresyncOutcome)
}

// The session seam (design-presync-attended §2.2, C-41): runSession is the
// pre-sync itself — presync.Run, set with supported by the macOS and Windows
// adapter file — and stateDirFn its per-user state folder. Tests in other packages
// replace them through OverrideSessionForTest with a scripted fake Thunderbird.
var (
	seamMu     sync.Mutex
	supported  bool
	runSession func(context.Context, presync.Config) presync.Result
	stateDirMu sync.Mutex
	stateDirFn = StateDir
)

// Supported reports whether this platform has a pre-sync adapter (macOS and
// Windows), so a caller can refuse an explicit request for the sync elsewhere.
func Supported() bool {
	seamMu.Lock()
	defer seamMu.Unlock()
	return supported
}

// OverrideSessionForTest makes the seam supported, with session in place of the
// real pre-sync and stateDir as its state folder, and returns the function that
// puts them back (C-41, C-79). It is exported only because the wizard, Desktop and
// command-line tests live in other packages; MA-898 fails if any non-test file
// calls it. An empty stateDir is a state folder that cannot be found, so SetHook
// fails as it does when StateDir does (the not-set-up case, §5.3, C-34).
func OverrideSessionForTest(session func(context.Context, presync.Config) presync.Result, stateDir string) (restore func()) {
	seamMu.Lock()
	oldSupported, oldSession := supported, runSession
	supported, runSession = true, session
	seamMu.Unlock()
	stateDirMu.Lock()
	oldDir := stateDirFn
	stateDirFn = func() (string, error) {
		if stateDir == "" {
			return "", errors.New("the pre-sync's state folder could not be found")
		}
		return stateDir, nil
	}
	stateDirMu.Unlock()
	return func() {
		seamMu.Lock()
		supported, runSession = oldSupported, oldSession
		seamMu.Unlock()
		stateDirMu.Lock()
		stateDirFn = oldDir
		stateDirMu.Unlock()
	}
}

// SetHook installs the pre-sync on opts.BeforeRead: one session over the run's
// discovered sources, mapped to the run's warnings (each side outcome too, B6),
// the session's Release, and the in-memory outcome the run hands its caller
// (design-presync-attended §4.1). It never fails the archive (R32 — app.Run
// recovers a panic in the hook). It installs nothing where the platform has no
// adapter, and nothing for an attended caller where presync.AttendedStartAllowed
// is false (C-49): there Run now does not sync, as no hook behaves today. An error
// means the state folder could not be found; the caller logs it and runs without
// the sync.
func SetHook(opts *app.Options, h HookOptions) error {
	seamMu.Lock()
	ok, session := supported, runSession
	seamMu.Unlock()
	if !ok {
		return nil
	}
	goos := h.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if h.Attended && !presync.AttendedStartAllowed(goos) {
		return nil
	}
	stateDirMu.Lock()
	dirFn := stateDirFn
	stateDirMu.Unlock()
	stateDir, err := dirFn()
	if err != nil {
		return err
	}
	cfg := presync.Config{
		Profile:      h.Profile,
		ProfileNamed: h.Profile != "",
		StateDir:     stateDir,
		Log:          h.Log,
		Attended:     h.Attended,
		Surface:      h.Surface,
		GOOS:         goos,
		Stop:         h.Stop,
		Phase:        h.Phase,
		KeepOpen:     h.KeepOpen,
	}
	if h.Attended {
		cfg.NeverQuit = !presync.AttendedQuitAllowed(goos)
		cfg.EndWait = h.EndWait
		// The first-attended-sync marker (§4.6, C-83): written when an attended
		// session reaches S5, which it reports as `starting`.
		cfg.Phase = func(phase string, since time.Time) {
			if phase == presync.PhaseStarting {
				markAttendedStarted(stateDir)
			}
			if h.Phase != nil {
				h.Phase(phase, since)
			}
		}
	}
	opts.BeforeRead = func(ctx context.Context, files []string) app.BeforeReadResult {
		c := cfg
		c.Inputs = files
		if h.OnStart != nil {
			h.OnStart()
		}
		// A session that panics still ends its caller's phase (§4.6, C-11): OnEnd is
		// told internal-error, then the panic goes on to app.Run, which recovers it.
		defer func() {
			if r := recover(); r != nil {
				if h.OnEnd != nil {
					h.OnEnd(app.PresyncOutcome{Code: presync.OutcomeInternalError.Code, Name: presync.OutcomeInternalError.Name,
						Class: presync.OutcomeInternalError.Class.String(), Left: string(presync.LeftNone),
						Line: presync.OutcomeLine(presync.OutcomeInternalError, c.Surface, goos, presync.LeftNone, 0, false)})
				}
				panic(r)
			}
		}()
		res := session(ctx, c)
		out := outcomeFor(res, c.Surface, goos)
		if h.OnEnd != nil {
			h.OnEnd(out)
		}
		return app.BeforeReadResult{Warnings: warnings(res), Release: res.Release, Outcome: &out}
	}
	return nil
}

// SetNotSetUp is what a caller installs when the job asks for the sync but SetHook
// could not prepare it (the state folder could not be found): a hook that runs no
// session and hands the run the outcome not-set-up, with its warning, so the run is
// recorded as a sync that did not happen — a scheduled run counts it missed — and
// never looks like "sync off" (design-presync-attended §5.3, C-34). It never fails
// the archive (R32). Where the platform has no adapter it installs nothing, as
// SetHook does.
func SetNotSetUp(opts *app.Options, h HookOptions) {
	if !Supported() {
		return
	}
	goos := h.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	opts.BeforeRead = func(context.Context, []string) app.BeforeReadResult {
		if h.OnStart != nil {
			h.OnStart()
		}
		res := presync.Result{Outcome: presync.OutcomeNotSetUp, Left: presync.LeftNone}
		out := outcomeFor(res, h.Surface, goos)
		if h.OnEnd != nil {
			h.OnEnd(out)
		}
		return app.BeforeReadResult{Warnings: warnings(res), Outcome: &out}
	}
}

// outcomeFor is the session's outcome as the run hands it to its caller, in
// memory only (design-presync-attended §4.1, §4.6): Synced from the outcome alone
// (C-72), and the end line in the caller's words, naming no path.
func outcomeFor(res presync.Result, surface, goos string) app.PresyncOutcome {
	left := res.Left
	if left == "" {
		left = presync.LeftNone
	}
	o := res.Outcome
	return app.PresyncOutcome{
		Code: o.Code, Name: o.Name, Class: o.Class.String(),
		Synced:    o.Synced(),
		Left:      string(left),
		Minimized: res.Minimized,
		Verdict:   res.Verdict.Name,
		Line:      presync.OutcomeLine(o, surface, goos, left, res.Waited, res.Minimized),
		Started:   res.Started,
		Waited:    res.Waited,
	}
}
