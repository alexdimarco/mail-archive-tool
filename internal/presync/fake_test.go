package presync

import (
	"context"
	"time"
)

// fakeEnv is a scripted env for driving the pure core in tests. The predicate
// tests (detection, ownership) set the static fields; the state-machine test
// sets sm=true and the timeline fields, making procs/liveness/digest advance
// with the clock as sleep() moves it.
type fakeEnv struct {
	nowT time.Time

	// static (detection tests)
	procs  []proc
	lockP  map[string]int
	lockOn map[string]bool
	alive  map[int]bool
	exeOf  map[int]string

	// session config + timeline (state-machine test)
	sm              bool
	realDigest      bool // when set, digest() does a real digestWatch over the roots
	boot            int64
	idle            float64
	idleOK          bool
	idleReturnsAt   time.Time // when set, idle drops to idleReturned at/after this time
	idleReturned    float64
	idleCalls       int // how many times idleSeconds was read (MA-895: attended reads it never)
	lockOK          bool
	releaseN        int    // C2: how many times releaseLock was called
	onRelease       func() // called after each releaseLock (lets a test wait for the latch)
	started         bool
	startCalls      int   // how many times startHidden was called (a launch request)
	recheckFail     bool  // when set, recheckProfile returns false (S5 TOCTOU refusal)
	startErr        error // when set, startHidden returns it and records no launch
	startedAt       time.Time
	quitted         bool
	quitCalls       int   // how many quit requests were made
	quitErr         error // when set, quit() returns it (the request was made but did not take)
	quitTarget      proc  // the proven child the session handed to quit (PC-W2)
	quitAt          time.Time
	quitCtxErr      error         // the quit context's Err() at the call (C-5)
	quitDeadline    time.Duration // the quit context's deadline, measured from the call on the real clock
	quitBlocks      bool          // quit waits until its context ends (a request held in flight)
	quitEndErr      error         // with quitBlocks: the quit context's Err() when quit returned
	sleepsAtQuit    int           // sleeps made before the quit request (any after it are the exit wait)
	quitAll         bool          // C3: quit closes all Thunderbird instances (macOS-like)
	extraProcs      []proc        // C3: additional Thunderbird procs present after launch (a second instance)
	unreadable      int           // C-1/SAF-9: Thunderbird pids the OS lists whose details cannot be read
	child           proc
	appearIn        time.Duration // child appears this long after startHidden
	exitIn          time.Duration // child exits this long after quit
	changeAt        time.Time     // the watch digest changes at/after this time
	dgBase          uint64
	dgChanged       uint64
	digestTruncated bool   // when set, digest() reports the walk was truncated
	digestHook      func() // called at the start of each digest() (lets a test cancel mid-S7)
	sleeps          int
	sleepsNoStop    int // waits made without the session context (A5: there must be none)

	// cost model (the budget test, MA-921): each probe moves the fake clock by
	// probeCost, the launcher by launchCost and the quit request by quitCost, so a
	// session's fake elapsed time is its worst real time.
	probeCost  time.Duration
	launchCost time.Duration
	quitCost   time.Duration
	// probeHook, when set, is called at the start of each probe with its name
	// ("procs", "alive", "exe", "boot", "idle", "raw"): it lets a test cancel, or
	// block the session, at an exact point.
	probeHook func(name string)
}

var _ env = (*fakeEnv)(nil)

func newFakeEnv() *fakeEnv {
	return &fakeEnv{
		lockP:  map[string]int{},
		lockOn: map[string]bool{},
		alive:  map[int]bool{},
		exeOf:  map[int]string{},
	}
}

func (f *fakeEnv) probe(name string) {
	if f.probeHook != nil {
		f.probeHook(name)
	}
	f.nowT = f.nowT.Add(f.probeCost)
}

func (f *fakeEnv) now() time.Time { return f.nowT }

// sleep advances the fake clock — but not when either context is already done,
// so a session test cannot pass by moving time after a cancel (A5).
func (f *fakeEnv) sleep(ctx, stop context.Context, d time.Duration) {
	if stop == nil {
		f.sleepsNoStop++
	}
	if (ctx != nil && ctx.Err() != nil) || (stop != nil && stop.Err() != nil) {
		return
	}
	f.nowT = f.nowT.Add(d)
	f.sleeps++
}

func (f *fakeEnv) thunderbirdProcs() []proc {
	f.probe("procs")
	return f.procsNow()
}

func (f *fakeEnv) procsNow() []proc {
	if !f.sm {
		return f.procs
	}
	if f.quitted && !f.nowT.Before(f.quitAt.Add(f.exitIn)) {
		return nil // exited after the quit
	}
	if f.started && !f.nowT.Before(f.startedAt.Add(f.appearIn)) {
		return append([]proc{f.child}, f.extraProcs...)
	}
	return nil
}

// rawThunderbirdCount counts every listed Thunderbird, the unreadable ones too.
func (f *fakeEnv) rawThunderbirdCount() (int, bool) {
	f.probe("raw")
	return len(f.procsNow()) + f.unreadable, true
}

func (f *fakeEnv) lockPID(profile string) (int, bool) {
	if !f.lockOn[profile] {
		return 0, false
	}
	return f.lockP[profile], true
}

func (f *fakeEnv) pidAlive(pid int) bool {
	f.probe("alive")
	if !f.sm {
		return f.alive[pid]
	}
	if pid != f.child.pid {
		return false
	}
	if f.quitted && !f.nowT.Before(f.quitAt.Add(f.exitIn)) {
		return false
	}
	return f.started && !f.nowT.Before(f.startedAt.Add(f.appearIn))
}

func (f *fakeEnv) procExe(pid int) (string, bool) {
	f.probe("exe")
	e, ok := f.exeOf[pid]
	return e, ok
}

func (f *fakeEnv) bootTime() int64 {
	f.probe("boot")
	return f.boot
}

func (f *fakeEnv) idleSeconds() (float64, bool) {
	f.probe("idle")
	f.idleCalls++
	if !f.idleReturnsAt.IsZero() && !f.nowT.Before(f.idleReturnsAt) {
		return f.idleReturned, f.idleOK // the owner came back
	}
	return f.idle, f.idleOK
}

func (f *fakeEnv) recheckProfile(canonical string) bool { return !f.recheckFail }

func (f *fakeEnv) startHidden(app, profile string) error {
	f.startCalls++
	f.nowT = f.nowT.Add(f.launchCost)
	if f.startErr != nil {
		return f.startErr
	}
	f.started, f.startedAt = true, f.nowT
	return nil
}

func (f *fakeEnv) quit(ctx context.Context, target proc) error {
	f.quitCalls++
	f.sleepsAtQuit = f.sleeps
	f.quitCtxErr = ctx.Err()
	if dl, ok := ctx.Deadline(); ok {
		f.quitDeadline = time.Until(dl)
	}
	if f.quitBlocks {
		<-ctx.Done()
		f.quitEndErr = ctx.Err()
	}
	f.nowT = f.nowT.Add(f.quitCost)
	f.quitted, f.quitAt, f.quitTarget = true, f.nowT, target
	return f.quitErr
}

func (f *fakeEnv) quitAffectsAllInstances() bool { return f.quitAll }

func (f *fakeEnv) digest(roots []string) (uint64, bool) {
	if f.digestHook != nil {
		f.digestHook()
	}
	if f.realDigest {
		return digestWatch(roots)
	}
	if !f.sm {
		return 0, false
	}
	if !f.nowT.Before(f.changeAt) {
		return f.dgChanged, f.digestTruncated
	}
	return f.dgBase, f.digestTruncated
}

func (f *fakeEnv) acquireLock(path string) (bool, error) { return f.lockOK, nil }
func (f *fakeEnv) releaseLock(path string) {
	f.releaseN++
	if f.onRelease != nil {
		f.onRelease()
	}
}
