package presync

import (
	"context"
	"errors"
	"io"
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

	// The attended mode (design-presync-attended §2.1; buildDeps copies them from the
	// Config, neverQuit and endWait only when attended).
	attended  bool
	neverQuit bool
	endWait   context.Context
	phase     func(phase string, since time.Time)
	// log receives the once-a-minute progress line while the session waits (A7).
	log io.Writer
	// markLeft tells the watchdog what an abandonment would leave Thunderbird as
	// right now, so its own result can say so (§2.6). nil means no watchdog.
	markLeft func(Left)
	// markSide tells the watchdog each side warning as the session meets it, so a
	// result it returns on its own carries them too (as1a-6). nil means no watchdog.
	markSide func(Outcome)
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

	// A8: the S7 verdict, what Thunderbird was left as, when it was started, how
	// long the wait for mail ran, and whether Windows' minimize succeeded.
	verdict   Outcome
	left      Left
	started   time.Time
	waited    time.Duration
	minimized bool
}

// result is the caller's view of a session: its outcome, its side warnings, the
// release of the profile lock it still holds (C2), and the A8 details.
func (r sessionResult) result() Result {
	return Result{Outcome: r.outcome, Side: r.side, Release: r.release,
		Verdict: r.verdict, Left: r.left, Started: r.started, Waited: r.waited, Minimized: r.minimized}
}

// windowMinimizer is the optional capability of an adapter that can only launch
// Thunderbird VISIBLY (Windows, where SW_HIDE is ignored) and wants the shown
// window minimized once the process is adopted. A truly-hidden adapter (macOS)
// does not implement it, so the S6 hook is a no-op there. Each reports whether the
// window was minimized (Result.Minimized, A8, C-56); attended picks the show mode
// (A14 (3), minimizeMode), a scheduled run keeps SW_MINIMIZE (§1.3).
type windowMinimizer interface {
	// minimizeMain is the first attempt, right after adoption: it waits up to
	// minimizeWait for the single main window, on the session context (C-64). It
	// addresses only the proven child (its pid and exact start, as4win-6).
	minimizeMain(ctx, stop context.Context, child proc, attended bool) bool
	// minimizeAgain is one more try with no wait, made on each S7 poll until one
	// attempt has succeeded, and never after (A14 (2), C-56; minimizeRetry).
	minimizeAgain(child proc, attended bool) bool
}

// minimizeRetry is the session's backstop-minimize retry rule (A14 (2), C-56): a
// minimize that has not yet succeeded is tried again, once per S7 poll, until one
// attempt succeeds — never after, so a person who restores our window to work in it
// is not fought. m is nil on an adapter that starts Thunderbird truly out of sight
// (macOS). MA-927's hash pins it with the Windows code it drives (as4win-4).
func minimizeRetry(m windowMinimizer, child proc, attended, minimized bool) bool {
	if m == nil || minimized {
		return minimized
	}
	return m.minimizeAgain(child, attended)
}

// runSession runs one pre-sync session, S1..S9 (design 4.5). It never fails the
// archive: every path ends in an Outcome the caller records, and the archive
// reads afterwards. In v1 it never quits a process that is not provably ours,
// and never signals or kills.
func runSession(ctx context.Context, d deps) (out sessionResult) {
	res := sessionResult{left: LeftNone}
	e := d.env
	enter := func(s string) { res.states = append(res.states, s) }

	// The session context (§2.6): the watchdog cancels it when it abandons the
	// session, and it carries the caller's Stop. Every wait watches it and the run's
	// ctx; the quit request's own context derives from it (C-5).
	sctx := d.sctx
	if sctx == nil {
		sctx = context.Background()
	}
	abandoned := func() bool { return d.abandoned != nil && d.abandoned() }
	// stopped: the Stop ended (a Lock, Shut down) or the watchdog gave up on this
	// session. Nothing a stopped session does may wait on, or ask anything of,
	// Thunderbird.
	stopped := func() bool { return sctx.Err() != nil || abandoned() }
	// halted: stopped, or the run itself was cancelled. A halted session launches
	// nothing (A11) and asks nothing to quit (A10 step 1).
	halted := func() bool { return ctx.Err() != nil || stopped() }
	// cancelQuit: the person pressed Cancel in an attended run (A4(ii)) — the run's
	// context ended with the Cancel button's cause and no Stop came with it. Any
	// other cause is A4(i): quit nothing, record as built.
	cancelQuit := func() bool {
		return d.attended && ctx.Err() != nil && errors.Is(context.Cause(ctx), ErrCancelAndQuit) && !stopped()
	}
	// setLeft records what the session leaves Thunderbird as, and tells the
	// watchdog, so a result it returns on its own says the same (§2.6).
	setLeft := func(l Left) {
		res.left = l
		if d.markLeft != nil {
			d.markLeft(l)
		}
	}
	// report tells the caller the phase, never once the session is abandoned (A15,
	// C-71): a late call must not overwrite the next run's note.
	report := func(phase string, since time.Time) {
		if d.phase != nil && !abandoned() {
			d.phase(phase, since)
		}
	}
	addSide := func(o Outcome) {
		for _, s := range res.side {
			if s.Code == o.Code {
				return
			}
		}
		res.side = append(res.side, o)
		if d.markSide != nil {
			d.markSide(o) // so a result the watchdog returns on its own carries it (as1a-6)
		}
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
	// defers it until after the read). A panic from here on ends the session
	// internal-error and hands the release on the same way, so the lock is never freed
	// behind the caller's read — neither directly nor past the watchdog's once-both
	// latch for an abandoned session (gate-2 pass 1, as1a-5; C2, C-77) — and never
	// leaks: the caller's Release frees it.
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
			res.outcome = OutcomeInternalError
			res.release = releaseLock
			out = res
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
	// A1: an attended run is one the person started at this computer (R38), so the
	// owner being here is the point, not a reason to skip: the idle clock is not
	// read at all. Every other check stays (§2.3).
	if !d.attended {
		if s, ok := e.idleSeconds(); !ok || s < d.idleAway.Seconds() {
			res.outcome = OutcomeOwnerActiveSkipped // the owner is at the computer
			return res
		}
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
	// The caller's `starting` hook runs (a marker write, the Desktop's lock), and the
	// watchdog is told a Thunderbird may be starting, BEFORE the second check, so the
	// check is the last step before the start (gate-2 pass 1, as1a-2): a Stop during
	// the hook starts nothing, and a watchdog that answers before the check says
	// nothing was started, which the check then makes true.
	report(PhaseStarting, launchAt)
	setLeft(LeftMayBeStarting) // from here an abandonment may leave a Thunderbird starting
	if halted() {
		clear()
		setLeft(LeftNone)
		res.outcome = OutcomeCancelled
		return res
	}
	if err := e.startHidden(d.app, d.profile); err != nil {
		clear() // write-ahead record removed; no launch to announce, no retry
		setLeft(LeftNone)
		switch {
		case errors.Is(err, errNoDesktopSession):
			res.outcome = OutcomeNoDesktopSession
		case errors.Is(err, errStartBlocked):
			// C-65: never a second launch without breakaway.
			res.outcome = OutcomeStartBlocked
		default:
			res.outcome = OutcomeCannotStart
		}
		return res
	}
	res.started = launchAt

	// S6 await-up — poll for a candidate that is provably ours.
	enter("S6")
	lc := launchCtx{app: d.app, profile: d.profile, launchAt: launchAt, snapshot: snapshot}
	var child proc
	// leaveOpenByRule ends an attended session that leaves its Thunderbird open
	// without asking it anything (A12, A16, A4(ii) under never-close): ownership is
	// re-verified, and only a Thunderbird still provably ours has its record cleared,
	// so the next run treats it as an open Thunderbird and leaves it alone (S4).
	// One still running but no longer provable as ours keeps its record for the next
	// run's recovery (left-open), the R31-safe direction. One that has gone (it exited
	// inside the last poll gap) with no other Thunderbird provably ours leaves nothing
	// open: its record is cleared and Left is exited — never "left open, quit it" for a
	// Thunderbird not running (gate-2 pass 1, as1b-2), and never "not started" for one
	// this session started (re-check, new item 2). One gone while another Thunderbird
	// still looks like ours (it relaunched itself, say) is open: left-open, the record
	// kept (re-check, new item 3).
	leaveOpenByRule := func(o Outcome) sessionResult {
		procs := e.thunderbirdProcs()
		switch m, ok := findOurs(procs, lc); {
		case ok && m.pid == child.pid:
			clear()
			setLeft(LeftOpenByRule)
		case !anyOurs(procs, lc) && child.pid != 0 && !e.pidAlive(child.pid):
			clear()
			setLeft(LeftExited)
		default:
			setLeft(LeftOpen)
		}
		res.outcome = o
		return res
	}
	// pollForChild is the S6 child poll after a Cancel under never-close (C-74): up
	// to the same 90 s await limit, on a short context of its own that a Stop also
	// ends — the cancelled run's own context has ended and is not waited on. None
	// proven by the limit, or a Stop, leaves `may-be-starting` and the `launching`
	// record for the next run's recovery.
	pollForChild := func() (proc, bool) {
		pctx, cancel := context.WithTimeout(sctx, d.awaitUp+d.awaitPoll)
		defer cancel()
		for !stopped() && pctx.Err() == nil {
			if m, ok := findOurs(e.thunderbirdProcs(), lc); ok {
				return m, true
			}
			if e.now().Sub(launchAt) >= d.awaitUp {
				break
			}
			e.sleep(pctx, sctx, d.awaitPoll)
		}
		return proc{}, false
	}
	for {
		if halted() {
			if cancelQuit() && d.neverQuit {
				// A4(ii) under never-close, S6 (C-74): keep polling for the child this
				// session started, so its record can be cleared; without the poll, the
				// `launching` record would make the next run take the person's own
				// Thunderbird for our leftover (SAF-5). Without NeverQuit (slice AS-Q
				// only) the Cancel quit follows the poll; until AS-Q it is A4(i).
				report(PhaseCancelling, e.now())
				if c, ok := pollForChild(); ok {
					// The proven child's identity is saved before the re-check, as on the
					// normal S6 path, so a re-check that fails keeps a record naming it —
					// never a pid-less `launching` one (gate-2 pass 1, as1b-3; SAF-5).
					child = c
					cur.ChildPID, cur.ChildStart, cur.ChildArgs = child.pid, child.start.Unix(), child.args
					_ = store(stateSyncing)
					setLeft(LeftOpen)
					return leaveOpenByRule(OutcomeCancelled)
				}
			}
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
	setLeft(LeftOpen)       // ours and up: an abandonment from here leaves it running, announced

	// An adapter that can only launch VISIBLY (Windows, where SW_HIDE is ignored)
	// minimizes the window it showed, so the pre-sync is a taskbar blip rather than
	// a focus-stealing popup (owner ruling 2026-10-23). Best-effort and bounded, and
	// its wait ends with the session context (C-64); a truly-hidden adapter (macOS)
	// does not implement this, so it is a no-op there.
	minimizer, canMinimize := e.(windowMinimizer)
	if canMinimize {
		res.minimized = minimizer.minimizeMain(ctx, sctx, child, d.attended)
	}

	// S7 wait quiet — poll the digest; quiet needs a first change (rev-3, L-M5),
	// and that first change must be mail data, not an index touch (MA-932).
	enter("S7")
	// A16 (OQ-6): "Stop waiting and back up now" ends the wait at once. Only an
	// attended run has it, and an EndWait that ended before the wait began changes
	// nothing (the route refuses outside `waiting`, §4.6). The waits in S7 watch a
	// context that EndWait also ends (A5). It is taken BEFORE `waiting` is reported,
	// so every press the route accepts — which it does from the report on — is seen
	// (gate-2 adversarial pass 1, as4web-3).
	endWait := d.endWait
	if !d.attended || (endWait != nil && endWait.Err() != nil) {
		endWait = nil
	}
	report(PhaseWaiting, up)
	waitCtx := sctx
	if endWait != nil {
		cctx, cancelWait := context.WithCancel(sctx)
		defer cancelWait()
		stopAfter := context.AfterFunc(endWait, cancelWait)
		defer stopAfter()
		waitCtx = endedBy{Context: cctx, also: endWait}
	}
	qt := newQuietTracker(up, d.cfg)
	watchTruncated := false
	maxMin := int(d.cfg.maxWait / time.Minute)
	saidMin := 0
	for {
		if halted() {
			res.waited = e.now().Sub(up)
			if cancelQuit() && d.neverQuit {
				// A4(ii) under never-close, S7: nothing is asked to close (OQ-1); the
				// outcome is cancelled, never synced, whatever it was left as (C-72).
				return leaveOpenByRule(OutcomeCancelled)
			}
			storeOrWarn(stateLeftRunning) // our Thunderbird is up; leave it, announce it
			res.outcome = OutcomeCancelled
			return res
		}
		if endWait != nil && endWait.Err() != nil {
			// A16: the person ended the wait. Never a close, on any platform and also
			// after AS-Q (Thunderbird may be mid-fetch); no verdict; the backup reads
			// what has arrived.
			res.waited = e.now().Sub(up)
			return leaveOpenByRule(OutcomeWaitEnded)
		}
		snap, trunc := e.digest(d.watch)
		if trunc {
			watchTruncated = true
		}
		now := e.now()
		qt.observe(snap, now)
		if o, done := qt.verdict(now); done {
			if watchTruncated {
				// The watch set exceeded its cap, so quiet was judged on the part
				// walked (§4.4.2): a note. Nothing arriving stays said as nothing
				// arriving (A13, C-68), never folded into the truncated "done".
				switch o {
				case OutcomeOK:
					o = OutcomeWatchTruncated
				case OutcomeNoActivity:
					o = OutcomeWatchTruncatedNoActivity
				}
			}
			res.outcome, res.verdict, res.waited = o, o, now.Sub(up)
			break
		}
		if !e.pidAlive(child.pid) {
			res.waited = now.Sub(up)
			setLeft(LeftNone)
			res.outcome = OutcomeExitedEarly // closed before quiet
			return res
		}
		// A7: once a minute, so a long wait is seen to be working (C-43).
		if n := int(now.Sub(up) / time.Minute); n > saidMin && n >= 1 {
			saidMin = n
			narrate(d.log, progressLine(n, maxMin))
		}
		// A14 (2), C-56: a minimize that has not yet succeeded is tried again on each
		// poll, until one succeeds — never after, so a person who restores our window
		// to work in it is not fought.
		res.minimized = minimizeRetry(minimizer, child, d.attended, res.minimized)
		e.sleep(ctx, waitCtx, d.pollEvery)
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
		res.waited = e.now().Sub(up) // A8: from "up" to the quit request
		switch err := e.quit(qctx, child); {
		case errors.Is(err, errQuitNotPosted):
			if !e.pidAlive(child.pid) {
				// Nothing was posted because our Thunderbird had already gone (it exited on
				// its own since the ownership re-check): nothing is left open, so the record
				// is cleared and the S7 verdict stands (gate-2 pass 1, as1a-1).
				clear()
				setLeft(LeftNone)
				return res.outcome, true
			}
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
	switch {
	case d.attended && d.neverQuit:
		// A12 (OQ-1): this platform's attended close is not allowed, so nothing is
		// ever asked to close. Ownership re-verified, the record cleared, the S7
		// verdict kept as the outcome (C-72), and the person told it was left open.
		return leaveOpenByRule(res.verdict)
	case d.attended:
		// A2: the person is here by definition, so the S8 idle re-check is skipped
		// and the shared pre-quit function decides (reached only from slice AS-Q).
	default:
		if s, ok := e.idleSeconds(); !ok || s < d.quitIdleAway.Seconds() {
			// The owner is back at the computer, or only briefly paused while reading
			// (C1: a HIGHER idle bar for the quit than for the start, so a normal
			// reading pause never quits a Thunderbird the owner may have adopted), or
			// the reading is unreadable (counts as active): leave the Thunderbird we
			// started, announce it, and never quit it on them.
			storeOrWarn(stateLeftRunning)
			res.outcome = OutcomeOwnerActive
			return res
		}
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
	setLeft(LeftClosed)
	return res
}

// endedBy is a context that is also done the moment `also` is: its Done channel
// is the embedded context's (which an after-function cancels when `also` ends),
// and its Err reports `also` at once, without waiting for that after-function to
// run, so no wait in S7 can begin after EndWait has ended (A5, A16).
type endedBy struct {
	context.Context
	also context.Context
}

func (c endedBy) Err() error {
	if err := c.Context.Err(); err != nil {
		return err
	}
	return c.also.Err()
}

// anyOurs reports whether any listed process meets the ownership criteria for this
// launch — one or several, where findOurs proves only a single one.
func anyOurs(ps []proc, lc launchCtx) bool {
	for _, p := range ps {
		if candidateIsOurs(p, lc) {
			return true
		}
	}
	return false
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
