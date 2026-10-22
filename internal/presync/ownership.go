package presync

import (
	"strings"
	"time"
)

// launchCtx is what S5 recorded about the Thunderbird this session launched
// (design section 4.4.3): the resolved executable we started, the canonical
// profile we passed as -profile, the launch time, and the snapshot of
// Thunderbird pids already present just before the launch (S4).
type launchCtx struct {
	app      string
	profile  string
	launchAt time.Time
	snapshot map[int]bool
}

// candidateIsOurs reports whether one candidate process is provably the
// Thunderbird this session launched (design 4.4.3): its executable equals the
// one we launched; its command line carries `-profile <canonical profile>`; its
// pid was NOT in the pre-launch snapshot; and its start time is at or after
// launchAt-2s. A candidate with no start time is NEVER ours — nothing may then
// be quit (presync_identity_unavailable). A candidate that started before the
// launch is the "already running" race, not ours.
func candidateIsOurs(c proc, lc launchCtx) bool {
	if c.exe != lc.app {
		return false
	}
	if !argsHaveProfile(c.args, lc.profile) {
		return false
	}
	if lc.snapshot[c.pid] {
		return false
	}
	if c.start.IsZero() {
		return false
	}
	return !c.start.Before(lc.launchAt.Add(-2 * time.Second))
}

// findOurs selects THE process that is ours from a candidate set: exactly one
// must pass candidateIsOurs (design 4.4.3, criterion 4). Zero or more than one
// qualifying candidate yields no match, so nothing is ever quit on ambiguity.
func findOurs(cands []proc, lc launchCtx) (proc, bool) {
	var match proc
	n := 0
	for _, c := range cands {
		if candidateIsOurs(c, lc) {
			match, n = c, n+1
		}
	}
	if n == 1 {
		return match, true
	}
	return proc{}, false
}

// argsHaveProfile reports whether a command line carries `-profile <profile>` as
// adjacent arguments (design 4.4.3 criterion 2, PC6). A Thunderbird on another
// profile, or one the owner opened plainly, never matches.
func argsHaveProfile(args, profile string) bool {
	f := strings.Fields(args)
	for i := 0; i+1 < len(f); i++ {
		if f[i] == "-profile" && f[i+1] == profile {
			return true
		}
	}
	return false
}
