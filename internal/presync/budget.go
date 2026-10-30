package presync

import "time"

// The bound every helper call on a session's path carries (design §4.7.1). They
// live in this untagged file, not in an adapter, so the whole-session budget below
// is computed from them on every platform, the Linux test gate included
// (design-presync-attended §2.6, C-2/T4).
//
// probeTimeout bounds every read-only helper (pgrep/ps/sysctl/ioreg/plutil on
// macOS), so a stuck system tool can never hang the pre-sync. launchTimeout bounds
// the launcher, which only asks the OS to start the app and returns at once; the
// long wait for the window to appear is S6, not this call. quitTimeout bounds the
// one quit request; the session builds that deadline (C-5) from its own context.
const (
	probeTimeout  = 5 * time.Second
	launchTimeout = 15 * time.Second
	quitTimeout   = 20 * time.Second
)

// probesOnPath is how many probeTimeout-bounded helper calls the longest session
// path makes OUTSIDE the time a poll loop already measures, counted from the code
// (design-presync-attended §2.6; the design's estimate was 9). A probe inside a
// loop is absorbed by the loop's own clock (S6 against awaitUp, S7 against maxWait,
// the S8 exit wait against quitGrace) except on the loop's last pass, which can
// overrun by one probe. On macOS, the longest path is:
//
//	resolve: plutil on each of the two candidate apps            2
//	S3: bootTime twice, the record match's process list          3
//	S4: idle; running check: a stale lock's pid, process list    3
//	S5: running check again: lock pid, process list; snapshot    3
//	S6: the last pass's process list                              1
//	S7: the last pass's liveness probe                            1
//	S8: idle                                                       1
//	preQuit: ownership's process list; the raw pid count          2
//	S8 exit wait: the last pass's liveness probe                  1
//
// One macOS process list is one probe: pgrep and every ps it leads to share one
// probeTimeout context. TestSessionBudgetCoversLongestPath drives that path on the
// fake clock with every probe taking its full bound, so an undercount here fails it.
const probesOnPath = 17

// budgetSlack is what the budget leaves over the counted path: it covers the S7
// folder walk's own time on a normal profile (a walk that cannot finish is what the
// watchdog exists to bound), and scheduling noise.
const budgetSlack = 60 * time.Second

// sessionBudget bounds a WHOLE pre-sync (design-presync-attended §2.6, C-2): every
// bound on the longest path plus the slack, computed from the same constants the
// session uses, so a constant changed later re-checks itself in
// TestSessionBudgetCoversLongestPath. It is the same in every mode, since the quit
// request keeps its 20 s bound (A3). With today's constants it is 1604 s; the 1440 s
// it replaces was shorter than the unattended worst path (T21).
func sessionBudget() time.Duration {
	return defAwaitUp + defAwaitPoll + // S6, and its last poll
		defMaxWait + defPollEvery + // S7, and its last poll
		probeTimeout*probesOnPath +
		launchTimeout + // S5's launcher
		quitTimeout + // S8's one request
		defQuitGrace + defAwaitPoll + // S8's exit wait, and its last poll
		budgetSlack
}
