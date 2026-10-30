package presync

import (
	"context"
	"errors"
	"os"
	"time"
)

// deps bundles the injected seams and settings a session needs. For TP1 the
// profile, app and watch set are already resolved — S1's planning (elevation,
// profile/app resolution, mode checks) is the macOS adapter's job (TP3); here
// the session orchestrates S1..S9 over them (design 4.5).
type deps struct {
	env          env
	cfg          quietConfig   // settle/quiet/maxWait for the watch (S7)
	app          string        // the resolved executable to launch and to match against
	profile      string        // the canonical profile path
	watch        []string      // the watch set (S7)
	statePath    string        // the state record file
	lockPath     string        // the per-profile session lock
	base         record        // the record template (profile/app/tool identity)
	awaitPoll    time.Duration // S6/S8 process poll
	awaitUp      time.Duration // S6 give-up
	pollEvery    time.Duration // S7 watch poll
	quitGrace    time.Duration // S8 give-up
	idleAway     time.Duration // owner-away threshold for the S4 start gate
	quitIdleAway time.Duration // owner-away threshold for the S8 QUIT gate — higher, so a reading pause never triggers a quit (C1)

	// sctx is the session context (design-presync-attended §2.6): derived from the
	// caller's Stop, and cancelled by the watchdog when it abandons the session.
	// Every wait and the quit request watch it. nil means context.Background().
	sctx context.Context
	// abandoned reports whether the watchdog has given up on this session (it
	// returned on its budget, a Stop or a cancel). An abandoned session starts no
	// quit request (A10 step 1). nil means never.
	abandoned func() bool
}

// sessionResult is what one pre-sync session produced: the final outcome, the
// state-transition sequence, and the record states written in order.
type sessionResult struct {
	outcome Outcome
	// side holds warn-class outcomes met on the way that the main outcome does not
	// carry: an unreadable state record at S3 (B6, A6, C-45) and a record that could
	// not be saved after the launch (A9). Each becomes a run warning.
	side    []Outcome
	states  []string
	records []recordState
	// release, when non-nil, releases the per-profile lock. runSession hands it to
	// the caller (via Result.Release → app.Run) so the lock is held across the pre-sync
	// AND the archive read, then released after the read (C2).
	release func()
}

// result is the caller's view of a session: its outcome, its side warnings, and
// the release of the profile lock it still holds (C2).
func (r sessionResult) result() Result {
	return Result{Outcome: r.outcome, Side: r.side, Release: r.release}
}

// windowMinimizer is the optional capability of an adapter that can only launch
// Thunderbird VISIBLY (Windows, where SW_HIDE is ignored) and wants the shown
// window minimized once the process is adopted. A truly-hidden adapter (macOS)
// does not implement it, so the S6 hook is a no-op there.
type windowMinimizer interface {
	minimizeMain(ctx, stop context.Context, pid int)
}

// runSession runs one pre-sync session, S1..S9 (design 4.5). It never fails the
// archive: every path ends in an Outcome the caller records, and the archive
// reads afterwards. In v1 it never quits a process that is not provably ours,
// and never signals or kills.
func runSession(ctx context.Context, d deps) sessionResult {
	res := sessionResult{}
	e := d.env
	enter := func(s string) { res.states = append(res.states, s) }

	// The session context (§2.6): the watchdog cancels it when it abandons the
	// session, and it carries the caller's Stop. Every wait watches it and the run's
	// ctx; the quit request's own context derives from it (C-5).
	sctx := d.sctx
	if sctx == nil {
		sctx = context.Background()
	}
	// halted: the run was cancelled, the Stop ended, or the watchdog gave up on this
	// session. A halted session launches nothing (A11) and asks nothing to quit
	// (A10 step 1); in AS1a every cancel cause is one that quits nothing.
	halted := func() bool {
		return ctx.Err() != nil || sctx.Err() != nil || (d.abandoned != nil && d.abandoned())
	}
	addSide := func(o Outcome) {
		for _, s := range res.side {
			if s.Code == o.Code {
				return
			}
		}
		res.side = append(res.side, o)
	}

	cur := d.base
	cur.Boot = e.bootTime()
	store := func(st recordState) error {
		cur.State = st
		if err := storeRecord(d.statePath, cur); err != nil {
			return err
		}
		res.records = append(res.records, st)
		return nil
	}
	// storeOrWarn saves a record a later run relies on to announce a Thunderbird
	// left open (quitting, left-running); a failed save never changes the outcome but
	// is told as the side warning presync_state_unsaved (A9).
	storeOrWarn := func(st recordState) {
		if store(st) != nil {
			addSide(OutcomeStateUnsaved)
		}
	}
	clear := func() { _ = os.Remove(d.statePath) }

	// S1 plan — resolution/validation is the adapter's (TP3); here it is given.
	enter("S1")

	// S2 profile lock — one try, no wait in v1.
	enter("S2")
	if ok, _ := e.acquireLock(d.lockPath); !ok {
		res.outcome = OutcomeProfileBusy
		return res
	}
	// C2: hold the per-profile lock through the ARCHIVE READ, not just the pre-sync
	// session — so a second scheduled run cannot pre-sync (and compact) this profile
	// while this run reads it. runSession does NOT release the lock itself on return;
	// it hands the release to the caller (res.release → Result.Release → app.Run, which
	// defers it until after the read). A panic here still releases it, so the lock never
	// leaks within the process.
	released := false
	releaseLock := func() {
		if !released {
			released = true
			e.releaseLock(d.lockPath)
		}
	}
	res.release = releaseLock
	defer func() {
		if r := recover(); r != nil {
			releaseLock()
			panic(r)
		}
	}()

	// S3 recover — announce-only in v1.
	enter("S3")
	rec, present, err := loadRecord(d.statePath)
	dec := applyRecovery(rec, present, err == errUnreadable, obs{boot: e.bootTime(), match: matchRecord(e, rec)})
	if dec.clear {
		clear()
	}
	if !dec.proceed {
		res.outcome = dec.outcome
		return res
	}
	if dec.outcome.Class == ClassWarn {
		// B6 (A6, C-45): a warning the recovery raised while the session goes on (an
		// unreadable record) reaches the caller beside the main outcome. A proceeding
		// note (matchOther's already-running) adds nothing.
		addSide(dec.outcome)
	}

	// S4 detect — owner-activity gate, the running gate, the pre-launch snapshot.
	enter("S4")
	if s, ok := e.idleSeconds(); !ok || s < d.idleAway.Seconds() {
		res.outcome = OutcomeOwnerActiveSkipped // the owner is at the computer
		return res
	}
	if sig := detectRunning(e, d.profile); sig.running {
		res.outcome = OutcomeAlreadyRunning // not started, not quit
		return res
	}

	// S5 launch — the write-ahead record is written BEFORE the start.
	enter("S5")
	if !e.recheckProfile(d.profile) {
		// §5.2 TOCTOU guard: the profile's canonical path changed (or vanished)
		// since S1 — refuse rather than launch on a swapped target. No record has
		// been written yet, so there is nothing to clear.
		res.outcome = OutcomeProfileUnusable
		return res
	}
	// C5: re-check for a running Thunderbird immediately before the launch — the owner
	// may have opened one in the gap since S4 (recheckProfile does filesystem work), so
	// never start a second instance beside theirs — and take the pre-launch snapshot as
	// LATE as possible, so any Thunderbird that appears after it is provably ours.
	if sig := detectRunning(e, d.profile); sig.running {
		res.outcome = OutcomeAlreadyRunning
		return res
	}
	snapshot := pidSet(e.thunderbirdProcs())
	launchAt := e.now()
	cur.LaunchAt = launchAt.Unix()
	// A11 (C-10): a cancel or a Stop that came first starts nothing — checked right
	// before the write-ahead record and again right before the start.
	if halted() {
		res.outcome = OutcomeCancelled
		return res
	}
	// A9 (C-8): launch only once the write-ahead record is on disk, so a Thunderbird
	// this session starts can always be told from the person's own next time.
	if store(stateLaunching) != nil {
		res.outcome = OutcomeCannotStart
		return res
	}
	if halted() {
		clear()
		res.outcome = OutcomeCancelled
		return res
	}
	if err := e.startHidden(d.app, d.profile); err != nil {
		clear() // write-ahead record removed; no launch to announce, no retry
		if errors.Is(err, errNoDesktopSession) {
			res.outcome = OutcomeNoDesktopSession
		} else {
			res.outcome = OutcomeCannotStart
		}
		return res
	}

	// S6 await-up — poll for a candidate that is provably ours.
	enter("S6")
	lc := launchCtx{app: d.app, profile: d.profile, launchAt: launchAt, snapshot: snapshot}
	var child proc
	for {
		if halted() {
			res.outcome = OutcomeCancelled // the launching record stays for recovery
			return res
		}
		if m, ok := findOurs(e.thunderbirdProcs(), lc); ok {
			child = m
			break
		}
		if e.now().Sub(launchAt) >= d.awaitUp {
			res.outcome = OutcomeDidNotStart // the record stays launching for the next run
			return res
		}
		e.sleep(ctx, sctx, d.awaitPoll)
	}
	up := e.now()
	cur.ChildPID, cur.ChildStart, cur.ChildArgs = child.pid, child.start.Unix(), child.args
	_ = store(stateSyncing) // best effort: a lost one leaves `launching`, which S3 still matches

	// An adapter that can only launch VISIBLY (Windows, where SW_HIDE is ignored)
	// minimizes the window it showed, so the pre-sync is a taskbar blip rather than
	// a focus-stealing popup (owner ruling 2026-10-23). Best-effort and bounded, and
	// its wait ends with the session context (C-64); a truly-hidden adapter (macOS)
	// does not implement this, so it is a no-op there.
	if m, ok := e.(windowMinimizer); ok {
		m.minimizeMain(ctx, sctx, child.pid)
	}

	// S7 wait quiet — poll the digest; quiet needs a first change (rev-3, L-M5).
	enter("S7")
	qt := newQuietTracker(up, d.cfg)
	watchTruncated := false
	for {
		if halted() {
			storeOrWarn(stateLeftRunning) // our Thunderbird is up; leave it, announce it
			res.outcome = OutcomeCancelled
			return res
		}
		sum, trunc := e.digest(d.watch)
		if trunc {
			watchTruncated = true
		}
		now := e.now()
		qt.observe(sum, now)
		if o, done := qt.verdict(now); done {
			if watchTruncated && (o == OutcomeOK || o == OutcomeNoActivity) {
				// the sync happened, but the watch set exceeded its cap: a note,
				// quiet judged on the part walked (§4.4.2).
				o = OutcomeWatchTruncated
			}
			res.outcome = o
			break
		}
		if !e.pidAlive(child.pid) {
			res.outcome = OutcomeExitedEarly // closed before quiet
			return res
		}
		e.sleep(ctx, sctx, d.pollEvery)
	}

	// preQuit is the ONE pre-quit function (A10, C-1); every quit goes through it.
	// It reports the outcome and true when the session ends here, or false once one
	// request was posted and the exit wait follows. In order: (1) a halted session —
	// cancelled, stopped or abandoned by the watchdog — asks nothing to quit; (2)
	// ownership re-verified, so a process not provably ours is never asked; (3) C3 on
	// a bundle-wide quit (macOS): the RAW pid count, an unreadable one counting too
	// (SAF-9), so the owner's Thunderbird is never closed beside ours; then the
	// `quitting` record, a last halted check, and one request on a context derived
	// from the session context with the 20 s bound (C-5), so the watchdog or a Stop
	// ends a request in flight; (7) a quit that posted nothing ends at once (C-60).
	// (Step 4, the attended `closing` phase and Keep-open read, lands with AS-Q.)
	preQuit := func() (Outcome, bool) {
		if halted() {
			storeOrWarn(stateLeftRunning)
			return OutcomeCancelled, true
		}
		if m, ok := findOurs(e.thunderbirdProcs(), lc); !ok || m.pid != child.pid {
			return OutcomeIdentityUnavailable, true // nothing provably ours now
		}
		if e.quitAffectsAllInstances() {
			if n, ok := e.rawThunderbirdCount(); !ok || n > 1 {
				// C3: the request would close EVERY Thunderbird, but only ONE pid is
				// proven ours — leave ours open rather than close the owner's too, and
				// announce it next run (MA-558). The archive still runs (R32).
				storeOrWarn(stateLeftRunning)
				return OutcomeQuitSkippedOthers, true
			}
		}
		storeOrWarn(stateQuitting)
		if halted() {
			storeOrWarn(stateLeftRunning)
			return OutcomeCancelled, true
		}
		qctx, qcancel := context.WithTimeout(sctx, quitTimeout)
		defer qcancel()
		switch err := e.quit(qctx, child); {
		case errors.Is(err, errQuitNotPosted):
			// Nothing was asked (no single main window, say): left open at once, with
			// no exit-grace wait and no text saying it was asked to close (C-60).
			storeOrWarn(stateLeftRunning)
			return OutcomeQuitNoWindow, true
		case errors.Is(err, errQuitNotPermitted):
			// Consent denied or the first-approval prompt unanswered: leave
			// Thunderbird, record the leftover so every later run announces it
			// (MA-558), never force it.
			storeOrWarn(stateLeftRunning)
			return OutcomeQuitNotPermitted, true
		}
		return Outcome{}, false
	}

	// S8 quit — the owner-activity re-check (owner ruling Q2): a Thunderbird we
	// started hidden is still reachable from the Dock, so if the owner has since
	// opened it, it is theirs now. Then the shared pre-quit function.
	enter("S8")
	if s, ok := e.idleSeconds(); !ok || s < d.quitIdleAway.Seconds() {
		// The owner is back at the computer, or only briefly paused while reading (C1:
		// a HIGHER idle bar for the quit than for the start, so a normal reading pause
		// never quits a Thunderbird the owner may have adopted), or the reading is
		// unreadable (counts as active): leave the Thunderbird we started, announce it,
		// and never quit it on them.
		storeOrWarn(stateLeftRunning)
		res.outcome = OutcomeOwnerActive
		return res
	}
	if o, end := preQuit(); end {
		res.outcome = o
		return res
	}
	quitAt := e.now()
	for e.pidAlive(child.pid) {
		if halted() {
			storeOrWarn(stateLeftRunning) // cancelled after the request; it may still close
			res.outcome = OutcomeCancelled
			return res
		}
		if e.now().Sub(quitAt) >= d.quitGrace {
			storeOrWarn(stateLeftRunning) // left running with identity, announced next run
			res.outcome = OutcomeQuitTimeout
			return res
		}
		e.sleep(ctx, sctx, d.awaitPoll)
	}

	// S9 post — clear the record (nothing announced on the happy path).
	enter("S9")
	clear()
	return res
}

func pidSet(ps []proc) map[int]bool {
	m := make(map[int]bool, len(ps))
	for _, p := range ps {
		m[p.pid] = true
	}
	return m
}

// matchRecord judges how a current Thunderbird process matches a state record's
// child identity (design 4.6 recovery), feeding applyRecovery at S3.
func matchRecord(e env, rec record) matchKind {
	procs := e.thunderbirdProcs()
	if len(procs) == 0 {
		return matchNoProcess
	}
	for _, p := range procs {
		if rec.ChildPID != 0 && p.pid == rec.ChildPID && p.exe == rec.App &&
			abs64(p.start.Unix()-rec.ChildStart) <= 1 && profileMatches(p, rec.Profile) {
			return matchExact
		}
	}
	if rec.State == stateLaunching && rec.ChildPID == 0 {
		cutoff := time.Unix(rec.LaunchAt, 0).Add(-2 * time.Second)
		for _, p := range procs {
			if p.exe == rec.App && !p.start.Before(cutoff) {
				return matchWeak
			}
		}
	}
	return matchOther
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
