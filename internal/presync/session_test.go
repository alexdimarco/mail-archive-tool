package presync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-552
// The pre-sync state machine's happy path (design 4.5), on a fake clock: S1..S9
// in order; the write-ahead record goes launching -> syncing -> quitting and is
// cleared at the end; a Thunderbird started hidden that fetches then goes quiet
// (the rev-3 rule) is asked to quit once and the run ends ok with the process
// gone.
func TestPresyncStateMachineHappyPath(t *testing.T) {
	base := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)
	const app = "/Applications/Thunderbird.app/Contents/MacOS/thunderbird"
	const profile = "/Users/x/profile.default"
	statePath := filepath.Join(t.TempDir(), "presync-abcd.json")

	f := newFakeEnv()
	f.sm = true
	f.boot = 1000
	f.idle, f.idleOK = 300, true
	f.lockOK = true
	f.appearIn, f.exitIn = 4*time.Second, 2*time.Second
	f.changeAt = base.Add(24 * time.Second)
	f.dgBase, f.dgChanged = 1, 2
	f.nowT = base
	f.child = proc{pid: 200, exe: app, start: base.Add(1 * time.Second), args: "thunderbird -profile " + profile}

	d := deps{
		env:       f,
		cfg:       quietConfig{settle: 10 * time.Second, quiet: 30 * time.Second, maxWait: 600 * time.Second},
		app:       app,
		profile:   profile,
		watch:     []string{"/w"},
		statePath: statePath,
		lockPath:  "/l",
		base:      record{Version: recordVersion, Profile: profile, App: app, ToolPID: 1, ToolStart: 1},
		awaitPoll: 2 * time.Second,
		awaitUp:   90 * time.Second,
		pollEvery: 10 * time.Second,
		quitGrace: 120 * time.Second,
		idleAway:  120 * time.Second,
	}

	res := runSession(context.Background(), d)

	if want := []string{"S1", "S2", "S3", "S4", "S5", "S6", "S7", "S8", "S9"}; !eqStrings(res.states, want) {
		t.Errorf("state sequence = %v, want %v", res.states, want)
	}
	if want := []recordState{stateLaunching, stateSyncing, stateQuitting}; !eqRecordStates(res.records, want) {
		t.Errorf("record states = %v, want %v", res.records, want)
	}
	if res.outcome != OutcomeOK {
		t.Errorf("outcome = %q, want ok", res.outcome.Name)
	}
	if !f.quitted {
		t.Errorf("Thunderbird was never asked to quit")
	}
	if f.quitTarget.pid != 200 {
		t.Errorf("quit target pid = %d, want the proven child 200 (PC-W2: the seam carries the target)", f.quitTarget.pid)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("the state record was not cleared at the end: %v", err)
	}
}

// covers: MA-552
// The S4 gates each stop the session before any launch (design 4.5): the owner
// at the computer, an already-running Thunderbird, and a busy profile lock.
func TestPresyncStateMachineGatesStopBeforeLaunch(t *testing.T) {
	const app, profile = "/a/thunderbird", "/p"
	mk := func(f *fakeEnv) deps {
		return deps{
			env: f, app: app, profile: profile, watch: []string{"/w"},
			statePath: filepath.Join(t.TempDir(), "s.json"), lockPath: "/l",
			base:      record{Version: recordVersion},
			cfg:       quietConfig{settle: time.Second, quiet: time.Second, maxWait: time.Minute},
			awaitPoll: time.Second, awaitUp: time.Minute, pollEvery: time.Second, quitGrace: time.Minute, idleAway: 120 * time.Second,
		}
	}

	t.Run("owner at the computer -> skipped, never launched", func(t *testing.T) {
		f := newFakeEnv()
		f.lockOK = true
		f.idle, f.idleOK = 30, true // under the 2-minute threshold
		if res := runSession(context.Background(), mk(f)); res.outcome != OutcomeOwnerActiveSkipped || f.started {
			t.Errorf("want owner-active-skipped and no launch; got %q started=%v", res.outcome.Name, f.started)
		}
	})

	t.Run("already running -> not started, not quit", func(t *testing.T) {
		f := newFakeEnv()
		f.lockOK = true
		f.idle, f.idleOK = 300, true
		f.procs = []proc{{pid: 9, exe: "/x/thunderbird"}}
		if res := runSession(context.Background(), mk(f)); res.outcome != OutcomeAlreadyRunning || f.started || f.quitted {
			t.Errorf("want already-running, no launch/quit; got %q started=%v quitted=%v", res.outcome.Name, f.started, f.quitted)
		}
	})

	t.Run("profile lock busy -> profile-busy", func(t *testing.T) {
		f := newFakeEnv() // lockOK defaults false
		if res := runSession(context.Background(), mk(f)); res.outcome != OutcomeProfileBusy || f.started {
			t.Errorf("want profile-busy and no launch; got %q", res.outcome.Name)
		}
	})
}

// covers: MA-571
// When the launcher reports no GUI session (startHidden -> errNoDesktopSession),
// S5 ends the session presync_no_desktop_session, removes the write-ahead record
// and reports no launch — and the archive, as ever, still runs afterwards.
func TestPresyncNoDesktopSession(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "s.json")
	f := newFakeEnv()
	f.sm = true
	f.boot = 1000
	f.idle, f.idleOK = 300, true // owner away
	f.lockOK = true
	f.startErr = errNoDesktopSession
	f.nowT = time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)

	d := deps{
		env: f, app: "/a/thunderbird", profile: "/p", watch: []string{"/w"},
		statePath: statePath, lockPath: "/l",
		base:      record{Version: recordVersion},
		cfg:       quietConfig{settle: time.Second, quiet: time.Second, maxWait: time.Minute},
		awaitPoll: time.Second, awaitUp: time.Minute, pollEvery: time.Second, quitGrace: time.Minute, idleAway: 120 * time.Second,
	}

	res := runSession(context.Background(), d)
	if res.outcome != OutcomeNoDesktopSession {
		t.Errorf("outcome = %q, want no-desktop-session", res.outcome.Name)
	}
	if f.started {
		t.Errorf("startHidden reported a launch despite the no-session failure")
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("the write-ahead record was not removed: %v", err)
	}
}

// covers: MA-570
// When Thunderbird will not quit (Automation consent denied -> quit returns
// errQuitNotPermitted), S8 ends presync_quit_not_permitted: exactly one quit is
// requested, Thunderbird is left running, and the record is stored left-running
// WITH the child identity so every later run announces the leftover (MA-558).
func TestPresyncQuitNotPermitted(t *testing.T) {
	base := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)
	const app = "/Applications/Thunderbird.app/Contents/MacOS/thunderbird"
	const profile = "/Users/x/profile.default"
	statePath := filepath.Join(t.TempDir(), "s.json")

	f := newFakeEnv()
	f.sm = true
	f.boot = 1000
	f.idle, f.idleOK = 300, true
	f.lockOK = true
	f.appearIn, f.exitIn = 4*time.Second, 2*time.Second
	f.changeAt = base.Add(24 * time.Second)
	f.dgBase, f.dgChanged = 1, 2
	f.nowT = base
	f.child = proc{pid: 200, exe: app, start: base.Add(1 * time.Second), args: "thunderbird -profile " + profile}
	f.quitErr = errQuitNotPermitted

	d := deps{
		env:       f,
		cfg:       quietConfig{settle: 10 * time.Second, quiet: 30 * time.Second, maxWait: 600 * time.Second},
		app:       app,
		profile:   profile,
		watch:     []string{"/w"},
		statePath: statePath,
		lockPath:  "/l",
		base:      record{Version: recordVersion, Profile: profile, App: app, ToolPID: 1, ToolStart: 1},
		awaitPoll: 2 * time.Second,
		awaitUp:   90 * time.Second,
		pollEvery: 10 * time.Second,
		quitGrace: 120 * time.Second,
		idleAway:  120 * time.Second,
	}

	res := runSession(context.Background(), d)
	if res.outcome != OutcomeQuitNotPermitted {
		t.Errorf("outcome = %q, want quit-not-permitted", res.outcome.Name)
	}
	if !f.quitted {
		t.Errorf("quit was never requested")
	}
	rec, present, err := loadRecord(statePath)
	if err != nil || !present {
		t.Fatalf("record missing after quit-not-permitted: present=%v err=%v", present, err)
	}
	if rec.State != stateLeftRunning {
		t.Errorf("record state = %q, want left-running", rec.State)
	}
	if rec.ChildPID != 200 {
		t.Errorf("record child pid = %d, want 200 (identity kept for announcement)", rec.ChildPID)
	}
	if n := len(res.records); n == 0 || res.records[n-1] != stateLeftRunning {
		t.Errorf("record states = %v, want to end left-running", res.records)
	}
}

// covers: MA-579
// The S8 owner-activity re-check (owner ruling Q2): a Thunderbird we started
// hidden that the owner has since opened (idle drops below the threshold by the
// time S8 runs) is theirs now — it is left open, the record is stored
// left-running with identity, the outcome is presync_owner_active, and NO quit
// is requested.
func TestPresyncOwnerActiveAtS8(t *testing.T) {
	base := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)
	const app = "/Applications/Thunderbird.app/Contents/MacOS/thunderbird"
	const profile = "/Users/x/profile.default"
	statePath := filepath.Join(t.TempDir(), "s.json")

	f := newFakeEnv()
	f.sm = true
	f.boot = 1000
	f.idle, f.idleOK = 300, true                 // owner away at S4 (300s > idleAway 120s)
	f.idleReturnsAt = base.Add(40 * time.Second) // owner back before S8 (~base+54s)
	f.idleReturned = 300                         // a 5-min reading pause: past idleAway (S4) but under quitIdleAway, so the S8 quit gate treats the owner as still present (C1)
	f.lockOK = true
	f.appearIn, f.exitIn = 4*time.Second, 2*time.Second
	f.changeAt = base.Add(24 * time.Second)
	f.dgBase, f.dgChanged = 1, 2
	f.nowT = base
	f.child = proc{pid: 200, exe: app, start: base.Add(1 * time.Second), args: "thunderbird -profile " + profile}

	d := deps{
		env:          f,
		cfg:          quietConfig{settle: 10 * time.Second, quiet: 30 * time.Second, maxWait: 600 * time.Second},
		app:          app,
		profile:      profile,
		watch:        []string{"/w"},
		statePath:    statePath,
		lockPath:     "/l",
		base:         record{Version: recordVersion, Profile: profile, App: app, ToolPID: 1, ToolStart: 1},
		awaitPoll:    2 * time.Second,
		awaitUp:      90 * time.Second,
		pollEvery:    10 * time.Second,
		quitGrace:    120 * time.Second,
		idleAway:     120 * time.Second,
		quitIdleAway: 600 * time.Second,
	}

	res := runSession(context.Background(), d)
	if res.outcome != OutcomeOwnerActive {
		t.Errorf("outcome = %q, want owner-active", res.outcome.Name)
	}
	if f.quitted {
		t.Error("C1: a quit was requested on a 5-min reading pause (idle past the S4 start gate but under the S8 quit gate)")
	}
	rec, present, err := loadRecord(statePath)
	if err != nil || !present {
		t.Fatalf("record missing after owner-active: present=%v err=%v", present, err)
	}
	if rec.State != stateLeftRunning || rec.ChildPID != 200 {
		t.Errorf("record = {state:%q childpid:%d}, want {left-running, 200}", rec.State, rec.ChildPID)
	}
}

// covers: MA-579
// An unreadable idle reading at S4 counts as the owner being active: the session
// ends owner-active-skipped and nothing is started — an unparseable reading must
// never be treated as idle.
func TestPresyncUnreadableIdleSkips(t *testing.T) {
	f := newFakeEnv()
	f.sm = true
	f.boot = 1000
	f.lockOK = true
	f.idle, f.idleOK = 999, false // unreadable: value ignored, ok=false
	f.nowT = time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)

	d := deps{
		env: f, app: "/a/thunderbird", profile: "/p", watch: []string{"/w"},
		statePath: filepath.Join(t.TempDir(), "s.json"), lockPath: "/l",
		base:      record{Version: recordVersion},
		cfg:       quietConfig{settle: time.Second, quiet: time.Second, maxWait: time.Minute},
		awaitPoll: time.Second, awaitUp: time.Minute, pollEvery: time.Second, quitGrace: time.Minute, idleAway: 120 * time.Second,
	}

	res := runSession(context.Background(), d)
	if res.outcome != OutcomeOwnerActiveSkipped {
		t.Errorf("outcome = %q, want owner-active-skipped", res.outcome.Name)
	}
	if f.started {
		t.Error("Thunderbird was started despite an unreadable idle reading")
	}
}

// covers: MA-560
// A cancelled run context ends the session promptly and never hangs the archive:
// cancelled while our Thunderbird is up (in S7), the session stores left-running
// with identity (announced next run) and ends cancelled, forcing no quit.
func TestPresyncCancelledInS7(t *testing.T) {
	base := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)
	const app = "/Applications/Thunderbird.app/Contents/MacOS/thunderbird"
	const profile = "/Users/x/profile.default"
	statePath := filepath.Join(t.TempDir(), "s.json")

	ctx, cancel := context.WithCancel(context.Background())
	f := newFakeEnv()
	f.sm = true
	f.boot = 1000
	f.idle, f.idleOK = 300, true
	f.lockOK = true
	f.appearIn, f.exitIn = 0, 2*time.Second // our child is present immediately
	f.changeAt = base.Add(1 * time.Hour)    // never goes quiet on its own
	f.dgBase, f.dgChanged = 1, 2
	f.nowT = base
	f.child = proc{pid: 200, exe: app, start: base.Add(1 * time.Second), args: "thunderbird -profile " + profile}
	f.digestHook = cancel // the first S7 digest cancels the run

	d := deps{
		env: f, cfg: quietConfig{settle: 10 * time.Second, quiet: 30 * time.Second, maxWait: 600 * time.Second},
		app: app, profile: profile, watch: []string{"/w"}, statePath: statePath, lockPath: "/l",
		base:      record{Version: recordVersion, Profile: profile, App: app, ToolPID: 1, ToolStart: 1},
		awaitPoll: 2 * time.Second, awaitUp: 90 * time.Second, pollEvery: 10 * time.Second, quitGrace: 120 * time.Second, idleAway: 120 * time.Second,
	}

	res := runSession(ctx, d)
	if res.outcome != OutcomeCancelled {
		t.Errorf("outcome = %q, want cancelled", res.outcome.Name)
	}
	if f.quitted {
		t.Error("a quit was forced on a cancelled session")
	}
	rec, present, err := loadRecord(statePath)
	if err != nil || !present || rec.State != stateLeftRunning || rec.ChildPID != 200 {
		t.Errorf("record = {present:%v state:%q pid:%d err:%v}, want left-running w/ pid 200", present, rec.State, rec.ChildPID, err)
	}
}

// covers: MA-561
// A clean session whose watch walk was truncated ends with the
// presync_watch_truncated note instead of a bare ok, so the owner learns the
// watch set exceeded its cap — and the sync still completed (Thunderbird quit).
func TestPresyncWatchTruncatedSurfaces(t *testing.T) {
	base := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)
	const app = "/Applications/Thunderbird.app/Contents/MacOS/thunderbird"
	const profile = "/Users/x/profile.default"
	statePath := filepath.Join(t.TempDir(), "s.json")

	f := newFakeEnv()
	f.sm = true
	f.boot = 1000
	f.idle, f.idleOK = 300, true
	f.lockOK = true
	f.appearIn, f.exitIn = 4*time.Second, 2*time.Second
	f.changeAt = base.Add(24 * time.Second)
	f.dgBase, f.dgChanged = 1, 2
	f.digestTruncated = true // the watch walk hit its cap
	f.nowT = base
	f.child = proc{pid: 200, exe: app, start: base.Add(1 * time.Second), args: "thunderbird -profile " + profile}

	d := deps{
		env:       f,
		cfg:       quietConfig{settle: 10 * time.Second, quiet: 30 * time.Second, maxWait: 600 * time.Second},
		app:       app,
		profile:   profile,
		watch:     []string{"/w"},
		statePath: statePath,
		lockPath:  "/l",
		base:      record{Version: recordVersion, Profile: profile, App: app, ToolPID: 1, ToolStart: 1},
		awaitPoll: 2 * time.Second,
		awaitUp:   90 * time.Second,
		pollEvery: 10 * time.Second,
		quitGrace: 120 * time.Second,
		idleAway:  120 * time.Second,
	}

	res := runSession(context.Background(), d)
	if res.outcome != OutcomeWatchTruncated {
		t.Errorf("outcome = %q, want watch-truncated", res.outcome.Name)
	}
	if !f.quitted {
		t.Error("the sync should still have completed (Thunderbird quit)")
	}
}

// covers: MA-562
// The S5 TOCTOU guard (§5.2): if the profile's canonical path changed between S1
// and the launch, S5 refuses with profile-unusable and starts nothing — no
// launch on a swapped target.
func TestPresyncProfileRecheckAtS5(t *testing.T) {
	f := newFakeEnv()
	f.sm = true
	f.boot = 1000
	f.idle, f.idleOK = 300, true // owner away
	f.lockOK = true
	f.recheckFail = true // the profile's canonical path changed since S1
	f.nowT = time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)

	d := deps{
		env: f, app: "/a/thunderbird", profile: "/p", watch: []string{"/w"},
		statePath: filepath.Join(t.TempDir(), "s.json"), lockPath: "/l",
		base:      record{Version: recordVersion},
		cfg:       quietConfig{settle: time.Second, quiet: time.Second, maxWait: time.Minute},
		awaitPoll: time.Second, awaitUp: time.Minute, pollEvery: time.Second, quitGrace: time.Minute, idleAway: 120 * time.Second,
	}

	res := runSession(context.Background(), d)
	if res.outcome != OutcomeProfileUnusable {
		t.Errorf("outcome = %q, want profile-unusable", res.outcome.Name)
	}
	if f.started {
		t.Error("Thunderbird was started despite the S5 profile re-check failing")
	}
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func eqRecordStates(a, b []recordState) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
