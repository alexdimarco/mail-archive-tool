package presync

import "time"

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
	lockOK          bool
	releaseN        int // C2: how many times releaseLock was called
	started         bool
	recheckFail     bool  // when set, recheckProfile returns false (S5 TOCTOU refusal)
	startErr        error // when set, startHidden returns it and records no launch
	startedAt       time.Time
	quitted         bool
	quitErr         error // when set, quit() returns it (the request was made but did not take)
	quitTarget      proc  // the proven child the session handed to quit (PC-W2)
	quitAt          time.Time
	quitAll         bool   // C3: quit closes all Thunderbird instances (macOS-like)
	extraProcs      []proc // C3: additional Thunderbird procs present after launch (a second instance)
	child           proc
	appearIn        time.Duration // child appears this long after startHidden
	exitIn          time.Duration // child exits this long after quit
	changeAt        time.Time     // the watch digest changes at/after this time
	dgBase          uint64
	dgChanged       uint64
	digestTruncated bool   // when set, digest() reports the walk was truncated
	digestHook      func() // called at the start of each digest() (lets a test cancel mid-S7)
	sleeps          int
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

func (f *fakeEnv) now() time.Time        { return f.nowT }
func (f *fakeEnv) sleep(d time.Duration) { f.nowT = f.nowT.Add(d); f.sleeps++ }

func (f *fakeEnv) thunderbirdProcs() []proc {
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

func (f *fakeEnv) lockPID(profile string) (int, bool) {
	if !f.lockOn[profile] {
		return 0, false
	}
	return f.lockP[profile], true
}

func (f *fakeEnv) pidAlive(pid int) bool {
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

func (f *fakeEnv) procExe(pid int) (string, bool) { e, ok := f.exeOf[pid]; return e, ok }
func (f *fakeEnv) bootTime() int64                { return f.boot }

func (f *fakeEnv) idleSeconds() (float64, bool) {
	if !f.idleReturnsAt.IsZero() && !f.nowT.Before(f.idleReturnsAt) {
		return f.idleReturned, f.idleOK // the owner came back
	}
	return f.idle, f.idleOK
}

func (f *fakeEnv) recheckProfile(canonical string) bool { return !f.recheckFail }

func (f *fakeEnv) startHidden(app, profile string) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started, f.startedAt = true, f.nowT
	return nil
}

func (f *fakeEnv) quit(target proc) error {
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
func (f *fakeEnv) releaseLock(path string)               { f.releaseN++ }
