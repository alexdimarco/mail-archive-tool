package presync

import "time"

// fakeEnv is a scripted env for driving the pure core in tests. Methods are
// added here as the interface grows.
type fakeEnv struct {
	nowT   time.Time
	procs  []proc
	lockP  map[string]int  // profile -> pid (<= 0 means a lock exists but names no parseable pid)
	lockOn map[string]bool // profile -> a lock exists
	alive  map[int]bool
	exeOf  map[int]string
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

func (f *fakeEnv) now() time.Time           { return f.nowT }
func (f *fakeEnv) thunderbirdProcs() []proc { return f.procs }

func (f *fakeEnv) lockPID(profile string) (int, bool) {
	if !f.lockOn[profile] {
		return 0, false
	}
	return f.lockP[profile], true
}

func (f *fakeEnv) pidAlive(pid int) bool          { return f.alive[pid] }
func (f *fakeEnv) procExe(pid int) (string, bool) { e, ok := f.exeOf[pid]; return e, ok }
