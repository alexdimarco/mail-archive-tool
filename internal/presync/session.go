package presync

import (
	"errors"
	"os"
	"time"
)

// deps bundles the injected seams and settings a session needs. For TP1 the
// profile, app and watch set are already resolved — S1's planning (elevation,
// profile/app resolution, mode checks) is the macOS adapter's job (TP3); here
// the session orchestrates S1..S9 over them (design 4.5).
type deps struct {
	env       env
	cfg       quietConfig   // settle/quiet/maxWait for the watch (S7)
	app       string        // the resolved executable to launch and to match against
	profile   string        // the canonical profile path
	watch     []string      // the watch set (S7)
	statePath string        // the state record file
	lockPath  string        // the per-profile session lock
	base      record        // the record template (profile/app/tool identity)
	awaitPoll time.Duration // S6/S8 process poll
	awaitUp   time.Duration // S6 give-up
	pollEvery time.Duration // S7 watch poll
	quitGrace time.Duration // S8 give-up
	idleAway  time.Duration // owner-away threshold
}

// sessionResult is what one pre-sync session produced: the final outcome, the
// state-transition sequence, and the record states written in order.
type sessionResult struct {
	outcome Outcome
	states  []string
	records []recordState
}

// runSession runs one pre-sync session, S1..S9 (design 4.5). It never fails the
// archive: every path ends in an Outcome the caller records, and the archive
// reads afterwards. In v1 it never quits a process that is not provably ours,
// and never signals or kills.
func runSession(d deps) sessionResult {
	res := sessionResult{}
	e := d.env
	enter := func(s string) { res.states = append(res.states, s) }

	cur := d.base
	cur.Boot = e.bootTime()
	store := func(st recordState) {
		cur.State = st
		_ = storeRecord(d.statePath, cur) // best-effort; a lost record never fails a run
		res.records = append(res.records, st)
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
	defer e.releaseLock(d.lockPath)

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
	snapshot := pidSet(e.thunderbirdProcs())

	// S5 launch — the write-ahead record is written BEFORE the start.
	enter("S5")
	launchAt := e.now()
	cur.LaunchAt = launchAt.Unix()
	store(stateLaunching)
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
		if m, ok := findOurs(e.thunderbirdProcs(), lc); ok {
			child = m
			break
		}
		if e.now().Sub(launchAt) >= d.awaitUp {
			res.outcome = OutcomeDidNotStart // the record stays launching for the next run
			return res
		}
		e.sleep(d.awaitPoll)
	}
	up := e.now()
	cur.ChildPID, cur.ChildStart, cur.ChildArgs = child.pid, child.start.Unix(), child.args
	store(stateSyncing)

	// S7 wait quiet — poll the digest; quiet needs a first change (rev-3, L-M5).
	enter("S7")
	qt := newQuietTracker(up, d.cfg)
	for {
		sum, _ := e.digest(d.watch)
		now := e.now()
		qt.observe(sum, now)
		if o, done := qt.verdict(now); done {
			res.outcome = o
			break
		}
		if !e.pidAlive(child.pid) {
			res.outcome = OutcomeExitedEarly // closed before quiet
			return res
		}
		e.sleep(d.pollEvery)
	}

	// S8 quit — BOTH re-checks immediately before the request (owner ruling Q2):
	// (1) re-verify ownership, so we never quit a process not provably ours; and
	// (2) re-check owner activity, because a Thunderbird we started hidden is
	// still reachable from the Dock, so if the owner has since opened it, it is
	// theirs now. Then one quit request.
	enter("S8")
	if m, ok := findOurs(e.thunderbirdProcs(), lc); !ok || m.pid != child.pid {
		res.outcome = OutcomeIdentityUnavailable // nothing provably ours now
		return res
	}
	if s, ok := e.idleSeconds(); !ok || s < d.idleAway.Seconds() {
		// The owner is back at the computer (or the reading is unreadable, which
		// counts as active): leave the Thunderbird we started, announce it, and
		// never quit it on them.
		store(stateLeftRunning)
		res.outcome = OutcomeOwnerActive
		return res
	}
	store(stateQuitting)
	if err := e.quit(); errors.Is(err, errQuitNotPermitted) {
		// Consent denied or the first-approval prompt unanswered: leave
		// Thunderbird, record the leftover so every later run announces it
		// (MA-558), never force it.
		store(stateLeftRunning)
		res.outcome = OutcomeQuitNotPermitted
		return res
	}
	quitAt := e.now()
	for e.pidAlive(child.pid) {
		if e.now().Sub(quitAt) >= d.quitGrace {
			store(stateLeftRunning) // left running with identity, announced next run
			res.outcome = OutcomeQuitTimeout
			return res
		}
		e.sleep(d.awaitPoll)
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
			abs64(p.start.Unix()-rec.ChildStart) <= 1 && argsHaveProfile(p.args, rec.Profile) {
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
