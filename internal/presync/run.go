package presync

import (
	"context"
	"errors"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// This file is the platform-neutral run scaffolding shared by every OS adapter
// (PC-W9): the whole-session watchdog, the deps assembly, and the watch-root /
// profile-hash helpers. Each adapter's Run is a thin wrapper around runWatchdog,
// so the R32 safety watchdog is written and reviewed ONCE, never copied per
// platform where it could drift.

// sessionCtl is what the watchdog hands the session it runs: the session context,
// which it cancels when it abandons the session, and the abandoned flag
// (design-presync-attended §2.6, C-3).
type sessionCtl struct {
	ctx       context.Context
	abandoned func() bool
	// markLeft is where the session says what an abandonment would leave
	// Thunderbird as; the watchdog's own result carries it (§2.6).
	markLeft func(Left)
	// markSide is where the session hands each side warning as it meets it (B6's
	// state-unreadable, A9's state-unsaved), so the watchdog's own result carries
	// them too (gate-2 pass 1, as1a-6).
	markSide func(Outcome)
}

func (s sessionCtl) isAbandoned() bool { return s.abandoned != nil && s.abandoned() }

// wire hands the session its watchdog: the session context, the abandoned flag and
// where it says what it would leave and which side warnings it met.
func (s sessionCtl) wire(d *deps) {
	d.sctx, d.abandoned, d.markLeft, d.markSide = s.ctx, s.abandoned, s.markLeft, s.markSide
}

// cancelPathWait bounds the watchdog's wait for the session's own Cancel path after
// the person's Cancel (cancelPathBudget; a var so a test can shrink it).
var cancelPathWait = cancelPathBudget()

// runWatchdog runs fn (one platform's whole pre-sync) under budget
// (sessionBudget, §2.6). filepath.WalkDir / os.ReadDir / DirEntry.Info cannot
// honour a context, so a watch root on an unresponsive mount would otherwise block
// forever; the watchdog gives up on such a session at this bound so the archive
// still runs (R32).
//
// It returns (1) the session's own result when it ends; (2) at once, `cancelled`,
// when stop ends, or ctx ends with any cause but ErrCancelAndQuit — with that cause
// (the person's Cancel) it waits for the session's own bounded Cancel path (the S6
// child poll under never-close, C-74) unless stop ends first; (3) at the budget,
// `internal-error`. Its own result's Left is what the session last said an
// abandonment would leave Thunderbird as. On (2) and (3) it marks the session abandoned
// and cancels the session context, so the session starts no quit request, a
// request in flight and every wait end, and the goroutine returns soon after
// (C-3). The profile lock the abandoned session holds is then released by a
// once-both latch: only when the session has returned AND the caller has called
// the returned Release after its read (C-77), so no other sync can compact the
// profile while this run reads it, and the long-lived Desktop never keeps it (T21).
func runWatchdog(ctx, stop context.Context, budget time.Duration, fn func(sessionCtl) Result) Result {
	parent := stop
	if parent == nil {
		parent = context.Background()
	}
	sctx, cancelSession := context.WithCancel(parent)
	var abandoned atomic.Bool
	var left atomic.Value
	left.Store(LeftNone)
	markLeft := func(l Left) { left.Store(l) }
	var sideMu sync.Mutex
	var sides []Outcome
	markSide := func(o Outcome) {
		sideMu.Lock()
		defer sideMu.Unlock()
		for _, s := range sides {
			if s.Code == o.Code {
				return
			}
		}
		sides = append(sides, o)
	}
	// sideSnapshot copies the side warnings the session has handed over so far, so a
	// result the watchdog returns on its own still carries them (as1a-6).
	sideSnapshot := func() []Outcome {
		sideMu.Lock()
		defer sideMu.Unlock()
		if len(sides) == 0 {
			return nil
		}
		return append([]Outcome(nil), sides...)
	}
	latch := &releaseLatch{}

	var mu sync.Mutex
	gaveUp, delivered := false, false
	ch := make(chan Result, 1)
	go func() {
		var r Result
		func() {
			// A panic in fn (resolve, launch, quit, an unsafe syscall read) is in THIS
			// goroutine; the caller's recover (app.Run's hook guard, MA-560) cannot catch
			// a panic in another goroutine, so without this the process would crash and
			// the archive would NOT run — an R32 breach (adversarial finding F4). Recover
			// to a warning instead, so the pre-sync still never gates the archive.
			defer func() {
				if p := recover(); p != nil {
					r = Result{Outcome: OutcomeInternalError, Side: sideSnapshot()}
				}
			}()
			r = fn(sessionCtl{ctx: sctx, abandoned: abandoned.Load, markLeft: markLeft, markSide: markSide})
		}()
		mu.Lock()
		if gaveUp {
			mu.Unlock()
			latch.sessionReturned(r.Release)
			return
		}
		delivered = true
		ch <- r
		mu.Unlock()
	}()

	timer := time.NewTimer(budget)
	defer timer.Stop()
	var ctxDone <-chan struct{}
	if ctx != nil {
		ctxDone = ctx.Done()
	}
	var out Outcome
	var cancelWait <-chan time.Time // armed by the person's Cancel (as1b-1)
wait:
	for {
		select {
		case r := <-ch:
			cancelSession()
			return r
		case <-timer.C:
			out = OutcomeInternalError
			break wait
		case <-ctxDone:
			if errors.Is(context.Cause(ctx), ErrCancelAndQuit) {
				// The person's Cancel: wait for the session's own Cancel path, but only up
				// to that path's bound, never the rest of the whole budget — a blocked
				// folder walk would otherwise hold the Cancel for up to 25 min (as1b-1).
				ctxDone = nil
				ct := time.NewTimer(cancelPathWait)
				defer ct.Stop()
				cancelWait = ct.C
				continue
			}
			out = OutcomeCancelled
			break wait
		case <-cancelWait:
			out = OutcomeCancelled
			break wait
		case <-parent.Done():
			out = OutcomeCancelled
			break wait
		}
	}
	mu.Lock()
	if delivered { // the session ended at the same moment: its own result stands
		mu.Unlock()
		cancelSession()
		return <-ch
	}
	gaveUp = true
	abandoned.Store(true)
	mu.Unlock()
	cancelSession()
	return Result{Outcome: out, Release: latch.callerReleased, Left: left.Load().(Left), Side: sideSnapshot()}
}

// releaseLatch frees an abandoned session's profile lock once both halves are
// done: the session has returned (handing over its release) and the caller has
// called Release after its read (C-77). Either order; at most once.
type releaseLatch struct {
	mu      sync.Mutex
	session bool
	caller  bool
	release func()
}

func (l *releaseLatch) sessionReturned(release func()) {
	l.mu.Lock()
	l.session, l.release = true, release
	fire := l.caller
	l.mu.Unlock()
	if fire && release != nil {
		release()
	}
}

func (l *releaseLatch) callerReleased() {
	l.mu.Lock()
	if l.caller {
		l.mu.Unlock()
		return
	}
	l.caller = true
	fire, release := l.session, l.release
	l.mu.Unlock()
	if fire && release != nil {
		release()
	}
}

// buildDeps assembles the session inputs from the resolved executable and profile
// and the config: the fixed watch roots (§4.4.2/§4.3), the state record and lock
// under the state directory (never the archive, §4.6), the record template, and
// the default timings, and the attended fields (design-presync-attended §2.1):
// NeverQuit and EndWait reach the session only for an attended run.
// Platform-neutral — each adapter resolves exe+profile its own way, then hands
// them here.
func buildDeps(e env, exe, profile string, cfg Config) deps {
	_ = os.MkdirAll(cfg.StateDir, 0o700)
	hash := profileHash(profile)
	var endWait context.Context
	if cfg.Attended {
		endWait = cfg.EndWait
	}
	return deps{
		attended:  cfg.Attended,
		neverQuit: cfg.Attended && cfg.NeverQuit,
		endWait:   endWait,
		phase:     cfg.Phase,
		log:       cfg.Log,
		env:       e,
		cfg:       quietConfig{settle: defSettle, quiet: defQuiet, maxWait: defMaxWait},
		app:       exe,
		profile:   profile,
		watch:     watchRoots(profile, cfg.Inputs),
		statePath: filepath.Join(cfg.StateDir, "presync-"+hash+".json"),
		lockPath:  filepath.Join(cfg.StateDir, "presync-"+hash+".lock"),
		base: record{
			Version:   recordVersion,
			Profile:   profile,
			App:       exe,
			ToolPID:   os.Getpid(),
			ToolStart: time.Now().Unix(),
		},
		awaitPoll:    defAwaitPoll,
		awaitUp:      defAwaitUp,
		pollEvery:    defPollEvery,
		quitGrace:    defQuitGrace,
		idleAway:     defIdleAway,
		quitIdleAway: defQuitIdleAway,
	}
}

// watchRoots are the mail directories a session watches for quiet (§4.4.2): the
// profile's ImapMail and Mail, plus any archive input that lies inside the
// profile. Only existing directories are returned; nothing outside the profile
// is ever watched (§4.3: inputs not inside it are archived as named, not watched).
func watchRoots(profile string, inputs []string) []string {
	var roots []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			seen[p] = true
			roots = append(roots, p)
		}
	}
	add(filepath.Join(profile, "ImapMail"))
	add(filepath.Join(profile, "Mail"))
	for _, in := range inputs {
		if c, err := filepath.EvalSymlinks(in); err == nil && underDir(profile, c) {
			add(c)
		}
	}
	return roots
}

// underDir reports whether path is dir itself or lies inside it.
func underDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func profileHash(profile string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(profile))
	return strconv.FormatUint(h.Sum64(), 16)
}
